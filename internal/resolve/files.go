package resolve

import (
	"path/filepath"
	"strings"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
)

// packDefault is the format `pack` uses unless told otherwise: it needs
// nothing beyond tar and gzip, which every Linux system has.
const packDefault = ".tar.gz"

// clean removes unneeded packages and cached package files on every source
// (or one), keeping each tool's own confirmation. Package lists are kept:
// the offline search needs them.
func (r *Resolver) clean(p grammar.Phrase) (Result, error) {
	var list []string
	if p.Source != "" {
		src, err := r.source(p.Source)
		if err != nil {
			return Result{}, err
		}
		if _, ok := r.Templates.Lookup(src, "clean", r.Sys.OSID); !ok {
			return Result{}, userErr("I don't know how to clean up %s yet", src)
		}
		list = []string{src}
	} else {
		for _, src := range r.present() {
			if src == r.Sys.Manager && r.Sys.Immutable {
				continue
			}
			if _, ok := r.Templates.Lookup(src, "clean", r.Sys.OSID); ok {
				list = append(list, src)
			}
		}
	}
	if len(list) == 0 {
		return Result{}, userErr("I don't know how to clean up anything on this system yet")
	}
	var cmds []string
	for _, src := range list {
		c, err := r.expand(src, "clean", nil)
		if err != nil {
			return Result{}, err
		}
		cmds = append(cmds, c)
	}
	return Result{Command: strings.Join(cmds, "; "), Phrase: p}, nil
}

// archiveFor finds the format of name by its extension (longest match).
func (r *Resolver) archiveFor(name string) (data.Archive, string, bool) {
	lower := strings.ToLower(name)
	var best data.Archive
	ext := ""
	for _, a := range r.Archives {
		for _, e := range a.Exts {
			if strings.HasSuffix(lower, e) && len(e) > len(ext) {
				best, ext = a, e
			}
		}
	}
	return best, ext, ext != ""
}

func (r *Resolver) knownExts(pack bool) string {
	var exts []string
	for _, a := range r.Archives {
		if !pack || a.Pack != "" {
			exts = append(exts, a.Exts[0])
		}
	}
	return strings.Join(exts, ", ")
}

func (r *Resolver) needs(progs []string, what string) error {
	for _, n := range progs {
		if _, err := r.lookPath(n); err != nil {
			return userErr("%s needs %s, which isn't installed (search %s)", what, n, n)
		}
	}
	return nil
}

func (r *Resolver) exists(path string) bool { return r.Exists != nil && r.Exists(path) }

// arg quotes a file name as a command argument; names starting with "-"
// get "./" so no tool mistakes them for options.
func arg(name string) string {
	if strings.HasPrefix(name, "-") {
		name = "./" + name
	}
	return Quote(name)
}

func (r *Resolver) unpack(p grammar.Phrase) (Result, error) {
	var cmds []string
	if p.Target != "" {
		cmds = append(cmds, "mkdir -p "+arg(p.Target))
	}
	for _, file := range p.Things {
		if !r.exists(file) {
			return Result{}, userErr("I found no file called %q here", file)
		}
		a, ext, ok := r.archiveFor(file)
		if !ok {
			return Result{}, userErr("I don't know how to unpack %q. I know: %s", file, r.knownExts(false))
		}
		if err := r.needs(a.UnpackNeeds, "unpacking "+ext+" files"); err != nil {
			return Result{}, err
		}
		tmpl := a.Unpack
		if p.Target != "" {
			if a.UnpackInto == "" {
				return Result{}, userErr("%s files unpack next to themselves; leave out \"into %s\"", ext, p.Target)
			}
			tmpl = a.UnpackInto
		}
		cmds = append(cmds, strings.NewReplacer("{file}", arg(file), "{dir}", arg(p.Target)).Replace(tmpl))
	}
	return Result{Command: strings.Join(cmds, " && "), Phrase: p}, nil
}

func (r *Resolver) pack(p grammar.Phrase) (Result, error) {
	for _, f := range p.Things {
		if !r.exists(f) {
			return Result{}, userErr("I found no file or folder called %q here", f)
		}
	}
	name := ""
	format := packDefault
	if t := p.Target; t != "" {
		_, ext, ok := r.archiveFor("x." + strings.TrimPrefix(t, "."))
		switch {
		case ok && strings.EqualFold("."+strings.TrimPrefix(t, "."), ext): // "as zip", "as .tar.gz"
			format = ext
		case ok: // "as photos.zip"
			name, format = t, ext
		default:
			return Result{}, userErr("I can pack as %s, or give a name like photos.zip", r.knownExts(true))
		}
	}
	a, _, _ := r.archiveFor("x" + format)
	if a.Pack == "" {
		return Result{}, userErr("I can't create %s files. I can make: %s", format, r.knownExts(true))
	}
	if name == "" {
		if len(p.Things) > 1 {
			return Result{}, userErr("what should the archive be called? For example: %s as archive%s", p.Text, a.Exts[0])
		}
		base := filepath.Base(filepath.Clean(p.Things[0]))
		if abs, err := filepath.Abs(p.Things[0]); err == nil {
			base = filepath.Base(abs) // "." → the folder's own name
		}
		name = base + a.Exts[0]
	}
	if r.exists(name) {
		return Result{}, userErr("%s already exists; choose another name, e.g.: pack %s as new-%s", name, strings.Join(p.Things, " and "), name)
	}
	if err := r.needs(a.PackNeeds, "packing "+a.Exts[0]+" files"); err != nil {
		return Result{}, err
	}
	files := make([]string, len(p.Things))
	for i, f := range p.Things {
		files[i] = arg(f)
	}
	cmd := strings.NewReplacer("{archive}", arg(name), "{files}", strings.Join(files, " ")).Replace(a.Pack)
	return Result{Command: cmd, Phrase: p}, nil
}

// trash returns the command that moves files to the trash. slash never
// deletes for good: without a trash tool it says so.
func (r *Resolver) trash(files []string) (string, error) {
	args := make([]string, len(files))
	for i, f := range files {
		args[i] = arg(f)
	}
	list := strings.Join(args, " ")
	if _, err := r.lookPath("gio"); err == nil {
		return "gio trash " + list, nil
	}
	if _, err := r.lookPath("trash-put"); err == nil {
		return "trash-put " + list, nil
	}
	return "", userErr("I found no trash here (gio or trash-put). Install one with: install trash-cli")
}

func (r *Resolver) delete(p grammar.Phrase) (Result, error) {
	for _, f := range p.Things {
		if r.exists(f) {
			continue
		}
		if r.installedAnywhere(f) {
			return Result{}, userErr("%s isn't a file here. To uninstall the program: remove %s", f, f)
		}
		return Result{}, userErr("I found no file or folder called %q here", f)
	}
	cmd, err := r.trash(p.Things)
	return Result{Command: cmd, Phrase: p}, err
}

func (r *Resolver) installedAnywhere(pkg string) bool {
	if r.Probe == nil {
		return false
	}
	for _, src := range r.present() {
		if _, ok := r.Probe.Installed(src, pkg); ok {
			return true
		}
	}
	return false
}

// removeFiles: `remove notes.txt`, where notes.txt is a file and no
// installed program: offer the trash instead (the user confirms).
func (r *Resolver) removeFiles(p grammar.Phrase) (Result, bool, error) {
	var files []string
	for _, t := range p.Things {
		if r.exists(t) && !r.installedAnywhere(t) {
			files = append(files, t)
		}
	}
	switch {
	case len(files) == 0:
		return Result{}, false, nil
	case len(files) < len(p.Things):
		return Result{}, true, userErr("%s is a file, not a program. To move it to the trash: delete %s", files[0], files[0])
	}
	cmd, err := r.trash(files)
	what := files[0] + " is a file"
	if len(files) > 1 {
		what = "these are files"
	}
	return Result{Command: cmd, Phrase: p, Confirm: true, Reason: what + ", not a program: moves it to the trash"}, true, err
}
