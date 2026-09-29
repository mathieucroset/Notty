package attach

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mathieucroset/notty/internal/links"
	"github.com/mathieucroset/notty/internal/vault"
)

// Unused returns the vault-relative paths, sorted, of every file under
// attachments/ that is not referenced by an image link in any note or in
// any trashed note (spec §6.2 "Clean unused attachments"). Links are
// resolved with links.Resolve, relative to each note's own path; for a
// trashed note that is its original path (read from the trash item's
// metadata), since trashed content keeps its original relative links.
func Unused(v *vault.Vault) ([]string, error) {
	used := map[string]bool{}

	if err := collectUsedInVault(v, used); err != nil {
		return nil, err
	}
	if err := collectUsedInTrash(v, used); err != nil {
		return nil, err
	}

	files, err := attachmentFiles(v)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, f := range files {
		if !used[f] {
			result = append(result, f)
		}
	}
	sort.Strings(result)
	return result, nil
}

// collectUsedInVault marks every attachment referenced by a note currently
// in the vault (excluding trash).
func collectUsedInVault(v *vault.Vault, used map[string]bool) error {
	tree, err := v.Tree()
	if err != nil {
		return fmt.Errorf("attach: scan vault: %w", err)
	}
	return walkNotes(v, tree, used)
}

func walkNotes(v *vault.Vault, n *vault.Node, used map[string]bool) error {
	if n.IsNote {
		content, err := v.Read(n.Path)
		if err != nil {
			return fmt.Errorf("attach: read %q: %w", n.Path, err)
		}
		markUsed(content, n.Path, used)
		return nil
	}
	for _, c := range n.Children {
		if err := walkNotes(v, c, used); err != nil {
			return err
		}
	}
	return nil
}

// collectUsedInTrash marks every attachment referenced by a note inside a
// trashed item, resolving its relative links against its original path.
func collectUsedInTrash(v *vault.Vault, used map[string]bool) error {
	items, err := v.TrashItems()
	if err != nil {
		return fmt.Errorf("attach: scan trash: %w", err)
	}
	for _, it := range items {
		notes, err := trashedNotes(v, it)
		if err != nil {
			return err
		}
		for originalRel, content := range notes {
			markUsed(content, originalRel, used)
		}
	}
	return nil
}

// trashedNotes returns the content of every ".md" file inside trash item
// it, keyed by the vault-relative path it would have had before being
// trashed (its "original path").
func trashedNotes(v *vault.Vault, it vault.TrashItem) (map[string]string, error) {
	result := map[string]string{}
	contentAbs := v.Abs(v.TrashContentPath(it))

	if !it.IsDir {
		if !strings.EqualFold(path.Ext(it.Name), ".md") {
			return result, nil
		}
		data, err := os.ReadFile(contentAbs)
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("attach: read trashed note %q: %w", it.OriginalPath, err)
		}
		result[it.OriginalPath] = string(data)
		return result, nil
	}

	err := filepath.WalkDir(contentAbs, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(contentAbs, abs)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		originalRel := path.Join(it.OriginalPath, filepath.ToSlash(rel))
		result[originalRel] = string(data)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("attach: scan trashed folder %q: %w", it.OriginalPath, err)
	}
	return result, nil
}

// markUsed records every non-external image link in content (belonging to
// the note at noteRel) that resolves to a vault file.
func markUsed(content, noteRel string, used map[string]bool) {
	for _, img := range links.FindImages(content) {
		rel, external := links.Resolve(img.Target, noteRel)
		if external || rel == "" {
			continue
		}
		used[rel] = true
	}
}

// attachmentFiles lists the vault-relative paths of every regular file
// under attachments/ (recursively), skipping in-flight save temp files. A
// vault with no attachments/ folder yet has none.
func attachmentFiles(v *vault.Vault) ([]string, error) {
	root := v.Abs(attachDir)
	var files []string
	err := filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".notty-tmp") {
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		files = append(files, path.Join(attachDir, filepath.ToSlash(rel)))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("attach: scan attachments: %w", err)
	}
	return files, nil
}
