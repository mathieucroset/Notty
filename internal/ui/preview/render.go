package preview

import (
	"errors"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/links"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// shared is the state every copy of a Model points to: caches and the
// Kitty bookkeeping. The maps are only touched from Update and the Set*
// methods (the Bubble Tea goroutine); the image cache is also used by the
// render command (one at a time) and is safe for that.
type shared struct {
	textCache map[textKey][]string
	imgCache  *imgrender.Cache
	cacheCap  int
	// sent holds the Kitty ids the terminal has: transmitted since it
	// last lost its images (startup or ResetKittyState) and not deleted.
	// It is the source of truth for deletes.
	sent map[uint32]bool

	// glamourCalls counts Glamour renders, for tests of the segment cache.
	glamourCalls atomic.Int64
}

// kittyIDs allocates Kitty image ids for every preview in the process, so
// two previews never share an id.
var kittyIDs struct {
	sync.Mutex
	last uint32
}

func allocKittyID() uint32 {
	kittyIDs.Lock()
	defer kittyIDs.Unlock()
	kittyIDs.last = imgrender.NextKittyID(kittyIDs.last)
	return kittyIDs.last
}

// textKey identifies one Glamour rendering of a text segment.
type textKey struct {
	hash    uint64
	width   int
	palette string
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// imgItem is one image of an Image segment, ready to lay out.
type imgItem struct {
	link links.ImageLink
	abs  string // absolute path of an existing file; "" for external, escaping or missing links
	name string
	// reason is set when the image cannot be shown ("missing", "external",
	// "unreadable", "too large"); it renders as a warning chip.
	reason string
	// rows are the rendered rows (half-blocks or Kitty placeholders); nil
	// with an empty reason means a plain chip (images off).
	rows    []string
	kittyID uint32
	// transmit is the Kitty transmission, present only when the render
	// found the id not yet sent; Update clears it once written. Cache
	// entries never hold it.
	transmit string
}

// block is the rendered form of one segment.
type block struct {
	text   []string   // Text: Glamour lines, blank edges trimmed
	images []*imgItem // Image
}

// textJob is a text segment to render, or its cached lines.
type textJob struct {
	key      textKey
	markdown string
	lines    []string
	cached   bool
}

// renderJob is everything a render command needs; it shares nothing
// mutable with the model except the concurrency-safe image cache.
type renderJob struct {
	gen           int
	sh            *shared
	notePath      string
	content       string
	width, height int
	palette       theme.Palette
	caps          imgrender.Caps
	vaultRoot     string
	segs          []Segment
	texts         []textJob // one per Text segment, in order
	// sent is a snapshot of the Kitty ids the terminal has: images whose
	// id is missing get their transmission (re)encoded.
	sent map[uint32]bool
	// encoded dedupes transmissions within the job.
	encoded map[uint32]string
}

// renderedMsg carries a finished render back to Update.
type renderedMsg struct {
	sh       *shared
	gen      int
	doc      *doc
	newTexts map[textKey][]string
}

// renderTickMsg fires when the debounce delay after a change has passed.
type renderTickMsg struct {
	sh  *shared
	gen int
}

// contentWidth is the width Glamour and images get: the pane minus one
// column of padding on each side.
func contentWidth(w int) int { return max(1, w-2) }

func (j renderJob) run() tea.Msg {
	msg := renderedMsg{sh: j.sh, gen: j.gen, newTexts: map[textKey][]string{}}
	d := &doc{
		gen:      j.gen,
		notePath: j.notePath,
		content:  j.content,
		width:    j.width,
		height:   j.height,
		segs:     j.segs,
		blocks:   make([]block, len(j.segs)),
	}
	j.renderTexts()
	j.encoded = map[uint32]string{}
	ti := 0
	for i, seg := range j.segs {
		if seg.Kind == Text {
			tj := j.texts[ti]
			ti++
			if !tj.cached {
				msg.newTexts[tj.key] = tj.lines
			}
			d.textKeys = append(d.textKeys, tj.key)
			d.blocks[i].text = tj.lines
			continue
		}
		for _, link := range seg.Images {
			d.blocks[i].images = append(d.blocks[i].images, j.image(link))
		}
	}
	d.layout()
	msg.doc = d
	return msg
}

// renderTexts renders the uncached text segments with Glamour, spread
// over up to GOMAXPROCS workers, each with its own renderer. Identical
// segments are rendered once.
func (j renderJob) renderTexts() {
	var todo []int
	first := map[textKey]int{}
	for i, tj := range j.texts {
		if tj.cached {
			continue
		}
		if _, dup := first[tj.key]; !dup {
			first[tj.key] = i
			todo = append(todo, i)
		}
	}
	if len(todo) > 0 {
		style := theme.GlamourStyle(j.palette) // registers the chroma style once
		workers := min(len(todo), runtime.GOMAXPROCS(0))
		var next atomic.Int64
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				tr, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(contentWidth(j.width)))
				if err != nil {
					tr = nil
				}
				for {
					k := int(next.Add(1)) - 1
					if k >= len(todo) {
						return
					}
					i := todo[k]
					j.texts[i].lines = j.glamour(tr, j.texts[i].markdown)
				}
			})
		}
		wg.Wait()
	}
	for i, tj := range j.texts {
		if !tj.cached && first[tj.key] != i {
			j.texts[i].lines = j.texts[first[tj.key]].lines
		}
	}
}

// glamour renders one text segment and trims the blank lines Glamour puts
// around a document. It falls back to the raw markdown lines on error.
func (j renderJob) glamour(tr *glamour.TermRenderer, md string) []string {
	j.sh.glamourCalls.Add(1)
	var lines []string
	if tr != nil {
		if out, err := tr.Render(md); err == nil {
			lines = strings.Split(out, "\n")
		}
	}
	if lines == nil {
		lines = strings.Split(md, "\n")
	}
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// image resolves, decodes (through the cache) and renders one image link.
// Kitty renderings are cached without their transmission; it is encoded
// only when the terminal does not have the id (j.sent).
func (j renderJob) image(link links.ImageLink) *imgItem {
	item := &imgItem{link: link, name: imageName(link.Target)}
	rel, external := links.Resolve(link.Target, j.notePath)
	switch {
	case external:
		item.reason = "external"
		return item
	case rel == "":
		item.reason = "missing"
		return item
	}
	item.name = path.Base(rel)
	abs := filepath.Join(j.vaultRoot, filepath.FromSlash(rel))
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		item.reason = "missing"
		return item
	}
	// Only existing files reach the image viewer.
	item.abs = abs

	proto := j.caps.Inline
	if proto == imgrender.ProtoOff {
		return item
	}
	if proto != imgrender.ProtoKitty {
		proto = imgrender.ProtoHalfBlocks
	}
	w, h, err := imgrender.Dimensions(item.abs)
	if err != nil {
		item.reason = decodeReason(err)
		return item
	}
	cols, rows := j.fit(w, h)
	key := imgrender.CacheKey{Path: item.abs, ModTime: fi.ModTime().UnixNano(), Cols: cols, Rows: rows, Proto: proto}
	r, cached := j.sh.imgCache.Get(key)
	needTransmit := proto == imgrender.ProtoKitty && (!cached || !j.sent[r.KittyID])
	if cached && needTransmit {
		if t, ok := j.encoded[r.KittyID]; ok {
			item.rows, item.kittyID, item.transmit = r.Rows, r.KittyID, t
			return item
		}
	}
	if cached && !needTransmit {
		item.rows, item.kittyID = r.Rows, r.KittyID
		return item
	}
	img, err := imgrender.Decode(item.abs)
	if err != nil {
		item.reason = decodeReason(err)
		return item
	}
	switch proto {
	case imgrender.ProtoKitty:
		id := r.KittyID
		if !cached {
			id = allocKittyID()
			r = imgrender.Rendered{Rows: imgrender.KittyPlaceholders(id, cols, rows), KittyID: id}
		}
		t := imgrender.KittyTransmit(img, id, cols, rows)
		if t == "" || len(r.Rows) == 0 {
			item.reason = "unreadable"
			return item
		}
		item.transmit = j.wrap(t)
		j.encoded[id] = item.transmit
	default:
		r = imgrender.Rendered{Rows: imgrender.HalfBlocks(img, cols, rows)}
		if len(r.Rows) == 0 {
			item.reason = "unreadable"
			return item
		}
	}
	if !cached {
		j.sh.imgCache.Put(key, r)
	}
	item.rows, item.kittyID = r.Rows, r.KittyID
	return item
}

// fit sizes an image: the pane width (minus padding) and at most 60% of
// the pane height, never larger than the image's own size in cells.
func (j renderJob) fit(w, h int) (cols, rows int) {
	cw, ch := j.caps.CellW, j.caps.CellH
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 16
	}
	maxCols := min(contentWidth(j.width), ceilDiv(w, cw))
	maxRows := min(max(3, j.height*60/100), ceilDiv(h, ch))
	return imgrender.FitCells(w, h, max(1, maxCols), max(1, maxRows), cw, ch)
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

func (j renderJob) wrap(seq string) string {
	if seq != "" && j.caps.TmuxPassthrough {
		return imgrender.WrapTmux(seq)
	}
	return seq
}

func decodeReason(err error) string {
	switch {
	case errors.Is(err, imgrender.ErrImageTooLarge):
		return "too large"
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	}
	return "unreadable"
}

// imageName is the chip label for a link target.
func imageName(target string) string {
	if strings.HasPrefix(strings.ToLower(target), "data:") {
		return "data URI"
	}
	if name := path.Base(strings.TrimRight(target, "/")); name != "." && name != "/" {
		return name
	}
	return target
}
