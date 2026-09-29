// Package setup plans and runs the git steps of the first-run wizard and of
// the palette's "Set up sync" (spec §4.6): it looks at the vault folder and
// the remote, turns the user's sync choice into a list of steps, and executes
// them. It has no UI code; the wizard shows each Step's Desc as progress.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mathieucroset/notty/internal/gitsync"
)

// Choice is the sync option picked in the wizard's second step.
type Choice int

const (
	// CreateGitHub creates a private GitHub repo with gh.
	CreateGitHub Choice = iota
	// ExistingURL syncs with an existing remote repository.
	ExistingURL
	// LocalOnly keeps a local git history without a remote.
	LocalOnly
)

func (c Choice) String() string {
	switch c {
	case CreateGitHub:
		return "CreateGitHub"
	case ExistingURL:
		return "ExistingURL"
	case LocalOnly:
		return "LocalOnly"
	}
	return fmt.Sprintf("Choice(%d)", int(c))
}

// VaultState is what InspectVault found at the vault path.
type VaultState int

const (
	// Missing: the path does not exist.
	Missing VaultState = iota
	// Empty: a directory with no entries other than a .notty/ directory.
	Empty
	// FilesNoRepo: a directory with files that is not a git repository.
	FilesNoRepo
	// Repo: the top level of a git working tree.
	Repo
)

func (s VaultState) String() string {
	switch s {
	case Missing:
		return "Missing"
	case Empty:
		return "Empty"
	case FilesNoRepo:
		return "FilesNoRepo"
	case Repo:
		return "Repo"
	}
	return fmt.Sprintf("VaultState(%d)", int(s))
}

// RemoteState is what InspectRemote found at the remote URL.
type RemoteState struct {
	HasHistory    bool
	DefaultBranch string // set when HasHistory
}

// StepKind identifies what a Step does.
type StepKind int

const (
	// StepInit runs `git init -b <Args[0]>`, creating the folder if needed.
	StepInit StepKind = iota
	// StepWriteGitignore makes sure .gitignore lists Notty's entries
	// (vault.EnsureGitignore) without committing.
	StepWriteGitignore
	// StepCommitAll stages everything except Notty's ignored files and
	// commits it with message Args[0] (nothing to commit is not an error).
	StepCommitAll
	// StepClone clones Args[0] into the vault folder, checking out branch
	// Args[1] (the remote default, whatever the remote HEAD says).
	StepClone
	// StepRemoteAdd adds Args[0] as origin (skipped when origin already has
	// that URL).
	StepRemoteAdd
	// StepFetch fetches origin.
	StepFetch
	// StepRenameBranch renames the current branch to Args[0] if it differs.
	StepRenameBranch
	// StepMergeUnrelated merges Args[0] with --allow-unrelated-histories;
	// conflicts stop the plan with the merge left in progress.
	StepMergeUnrelated
	// StepPush pushes the current branch to origin and sets its upstream.
	StepPush
	// StepGHCreate runs `gh repo create <Args[0]> --private --source .
	// --remote origin --push`.
	StepGHCreate
	// StepEnsureGitignoreCommit runs vault.EnsureGitignore and commits
	// .gitignore with message Args[0] if it changed. It does nothing while a
	// merge is in progress.
	StepEnsureGitignoreCommit
)

var stepNames = [...]string{"Init", "WriteGitignore", "CommitAll", "Clone", "RemoteAdd", "Fetch",
	"RenameBranch", "MergeUnrelated", "Push", "GHCreate", "EnsureGitignoreCommit"}

func (k StepKind) String() string {
	if k >= 0 && int(k) < len(stepNames) {
		return stepNames[k]
	}
	return fmt.Sprintf("StepKind(%d)", int(k))
}

// Step is one action of a setup plan.
type Step struct {
	Kind StepKind
	Args []string
	// Desc is a sentence for the wizard's progress list.
	Desc string
}

// Request is what the wizard collected.
type Request struct {
	Vault    string // vault folder
	Choice   Choice
	URL      string // ExistingURL: the remote
	RepoName string // CreateGitHub: the repo name ("notes" or "owner/notes")
	Host     string // short hostname for commit messages; defaults to this machine's
	// GHAvailable reports that gh is installed and authenticated
	// (GH.Available); CreateGitHub is refused without it.
	GHAvailable bool
}

// ErrGHUnavailable is returned by Plan for CreateGitHub when gh is not
// installed or not authenticated.
var ErrGHUnavailable = errors.New("setup: gh is not installed or not authenticated")

// nottyDir is ignored when deciding whether a folder is empty (plan A1).
const nottyDir = ".notty"

// InspectVault reports the state of the vault folder at path. A directory
// whose only entry is a .notty/ directory counts as Empty. A directory nested
// inside another repository is not a Repo.
func InspectVault(path string) (VaultState, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return 0, fmt.Errorf("setup: inspect vault: %w", err)
	}
	if !fi.IsDir() {
		return 0, fmt.Errorf("setup: inspect vault: %s is not a directory", path)
	}
	if gitsync.Open(path).IsRepo() {
		return Repo, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, fmt.Errorf("setup: inspect vault: %w", err)
	}
	for _, e := range entries {
		if !(e.Name() == nottyDir && e.IsDir()) {
			return FilesNoRepo, nil
		}
	}
	return Empty, nil
}

// InspectRemote runs `git ls-remote` on url. Network and authentication
// failures wrap gitsync.ErrNetwork and gitsync.ErrAuth.
func InspectRemote(ctx context.Context, url string) (RemoteState, error) {
	has, branch, err := gitsync.LsRemote(ctx, url)
	if err != nil {
		return RemoteState{}, fmt.Errorf("setup: inspect remote: %w", err)
	}
	return RemoteState{HasHistory: has, DefaultBranch: branch}, nil
}

// Plan returns the steps for req given the vault and remote states (spec
// §4.6). rs is only used for ExistingURL.
//
// Every flow ends with (or, when it publishes, just before its push has)
// StepEnsureGitignoreCommit. New repositories get .gitignore written before
// their first commit. When local files are merged with remote history,
// .gitignore is only ensured after the merge, so that a locally generated
// .gitignore cannot conflict with the remote's; the commit before the merge
// still leaves out Notty's ignored files.
func Plan(req Request, vs VaultState, rs RemoteState) ([]Step, error) {
	if req.Vault == "" {
		return nil, errors.New("setup: plan: no vault folder")
	}
	if vs < Missing || vs > Repo {
		return nil, fmt.Errorf("setup: plan: unknown vault state %v", vs)
	}
	host := req.Host
	if host == "" {
		host = defaultHost()
	}
	isRepo := vs == Repo
	initial := "Initial commit · " + host
	pending := "Commit pending changes · " + host
	ensure := step(StepEnsureGitignoreCommit, "Checking .gitignore", "Update .gitignore · "+host)

	var steps []Step
	add := func(s ...Step) { steps = append(steps, s...) }
	newRepo := func(branch string) {
		add(step(StepInit, "Creating a git repository on branch "+branch, branch),
			step(StepWriteGitignore, "Writing .gitignore"),
			step(StepCommitAll, "Creating the initial commit", initial))
	}

	switch req.Choice {
	case CreateGitHub:
		if !req.GHAvailable {
			return nil, ErrGHUnavailable
		}
		if err := checkRepoName(req.RepoName); err != nil {
			return nil, err
		}
		if isRepo {
			add(step(StepWriteGitignore, "Writing .gitignore"),
				step(StepCommitAll, "Committing pending changes", pending))
		} else {
			newRepo("main")
		}
		add(ensure, step(StepGHCreate, "Creating private GitHub repo "+req.RepoName, req.RepoName))

	case ExistingURL:
		if err := checkURL(req.URL); err != nil {
			return nil, err
		}
		def := rs.DefaultBranch
		if rs.HasHistory {
			if def == "" || strings.HasPrefix(def, "-") || strings.ContainsAny(def, " \t\n") {
				return nil, fmt.Errorf("setup: plan: invalid remote default branch %q", def)
			}
		}
		remoteAdd := step(StepRemoteAdd, "Adding remote origin "+req.URL, req.URL)
		push := step(StepPush, "Pushing to origin")
		fetch := step(StepFetch, "Fetching from origin")
		merge := step(StepMergeUnrelated, "Merging origin/"+def, "origin/"+def)
		switch {
		case !isRepo && vs != FilesNoRepo && rs.HasHistory:
			add(step(StepClone, "Cloning "+req.URL, req.URL, def), ensure)
		case !isRepo && !rs.HasHistory:
			newRepo("main")
			add(remoteAdd, ensure, push)
		case vs == FilesNoRepo: // with history
			add(step(StepInit, "Creating a git repository on branch "+def, def),
				step(StepCommitAll, "Committing existing files", initial),
				remoteAdd, fetch, merge, ensure, push)
		case !rs.HasHistory: // repo
			add(step(StepWriteGitignore, "Writing .gitignore"),
				step(StepCommitAll, "Committing pending changes", pending),
				remoteAdd, ensure, push)
		default: // repo with history
			add(step(StepCommitAll, "Committing pending changes", pending),
				remoteAdd, fetch,
				step(StepRenameBranch, "Using the remote's default branch "+def, def),
				merge, ensure, push)
		}

	case LocalOnly:
		if !isRepo {
			newRepo("main")
		}
		add(ensure)

	default:
		return nil, fmt.Errorf("setup: plan: unknown choice %v", req.Choice)
	}
	return steps, nil
}

func step(k StepKind, desc string, args ...string) Step {
	return Step{Kind: k, Args: args, Desc: desc}
}

func checkURL(u string) error {
	switch {
	case strings.TrimSpace(u) == "":
		return errors.New("setup: plan: no remote URL")
	case strings.HasPrefix(u, "-"):
		return fmt.Errorf("setup: plan: invalid remote URL %q", u)
	}
	return nil
}

// checkRepoName accepts "name" or "owner/name" made of the characters GitHub
// allows.
func checkRepoName(name string) error {
	if name == "" {
		return errors.New("setup: plan: no GitHub repo name")
	}
	parts := strings.Split(name, "/")
	ok := len(parts) <= 2
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, "-") || strings.HasPrefix(p, ".") {
			ok = false
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				ok = false
			}
		}
	}
	if !ok {
		return fmt.Errorf("setup: plan: invalid GitHub repo name %q", name)
	}
	return nil
}

func defaultHost() string {
	h, _ := os.Hostname()
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	if h == "" {
		return "unknown"
	}
	return h
}
