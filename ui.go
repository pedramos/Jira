package main

import (
	"bytes"
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"
	"sync"
	"text/tabwriter"

	"9fans.net/go/acme"
	jira "github.com/andygrunwald/go-jira"
)

type win struct {
	*acme.Win
	Title string
	// All windows should have put/get
	reload func(*win)
	put    func(*win)

	// If an issue window, all these should exist
	Issue      bool
	tr         map[string]string
	issueState string
	headers    *headers
}

func (w *win) Clear() {
	w.Addr(",")
	w.Write("data", nil)
}

func (w *win) Reload() {
	if w.reload != nil {
		w.reload(w)
	}
}

func (w *win) Put() {
	if w.put != nil {
		w.put(w)
	}
}

const addrdelim = "/[ \t\\n<>()\\[\\]\"']/"

// This is modeled on a similar set of functions that seem to be in every acme program.
func (w *win) expand(e *acme.Event) {
	w.Addr("#%d-%s", e.Q0, addrdelim)
	l, _, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
	}

	w.Addr("#%d+%s", e.Q0, addrdelim)
	r, _, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
	}

	if r < l {
		l = 0
	} else {
		l++
	}

	w.Addr("#%d,#%d", l, r)
	data, err := w.ReadAll("xdata")
	if err != nil {
		log.Println(err)
	}
	e.Q0 = l
	e.Q1 = r
	e.Text = data
}

func (w *win) loop(ui *UI) {
	defer ui.exit(w.Title)
	for e := range w.EventChan() {
		switch e.C2 {
		case 'x', 'X': // button 2
			debug("event: %q %q\n", e.C2, string(e.Text))
			cmd := strings.TrimSpace(string(e.Text))
			switch cmd {
			case "Put":
				w.Put()
				fallthrough
			case "Get":
				w.Reload()
				continue
			case "New":
				ui.createIssue()
				continue
			case "Search":
				// launch a search window, or tell the search window to re-scan its query line
				ui.err("Asked to Search, but I'm too dumb D:\n")
				continue
			}
			if w.Issue {
				if id, ok := w.tr[cmd]; ok {
					debug("transition: %q %q\n", cmd, id)
					ui.transitionIssue(w, id)
					w.Reload()
					continue
				}
			}
			if strings.HasPrefix(cmd, "Search") {
				query := strings.TrimSpace(cmd[6:])
				// do a search and return results
				_ = query
			}
		case 'l', 'L': // button 3
			debug("event: %x %q %q\n", e.Flag, e.C2, string(e.Text))
			if ui.look(string(e.Text)) {
				// we found it, or made it!
				continue
			}
			// this could be a built-in, punt without expanding
			if e.Flag&0x1 != 0 {
				break
			}
			w.expand(e)
			debug("expanded: %d-%d %q\n", e.Q0, e.Q1, string(e.Text))
			if ui.look(string(e.Text)) {
				continue
			}
		}
		w.WriteEvent(e)
	}
}

func (w *win) comment() string {
	w.Addr(`#0`)
	w.Addr(`/^\n/,/^\nReported by/`)
	q0, q1, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
		return ""
	}
	q1 -= len("\nReported by")
	w.Addr(`#%d,#%d`, q0, q1)
	b, err := w.ReadAll("xdata")
	if err != nil {
		log.Println(err)
		return ""
	}
	s := strings.TrimSpace(string(b))
	if s != "" {
		s += "\n"
	}
	return s
}

type UI struct {
	sync.Mutex
	win    map[string]*win
	exited chan struct{}
	projRe *regexp.Regexp

	j      *jira.Client
	prefix string
}

func (u *UI) err(s string) {
	if !strings.HasSuffix(s, "\n") {
		s = s + "\n"
	}
	w := u.show("+Errors")
	if w == nil {
		w = u.new("+Errors")
	}
	w.Fprintf("body", "%s", s)
	w.Addr("$")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func New(prefix string, j *jira.Client) (*UI, error) {
	prefix = path.Join("/jira", prefix)
	u := &UI{}

	if u.win == nil {
		u.win = make(map[string]*win)
	}
	if u.prefix == "" {
		u.prefix = prefix
	}
	u.exited = make(chan struct{})

	l, _, err := j.Project.GetList()
	if err != nil {
		close(u.exited)
		return nil, err
	}

	var r []string
	for _, p := range *l {
		r = append(r, "("+p.Key+")")
	}

	u.j = j
	u.projRe = regexp.MustCompile("^(" + strings.Join(r, "|") + ")-[0-9]+")
	return u, nil
}

func (u *UI) new(title string) *win {
	u.Lock()
	defer u.Unlock()
	var err error
	w := &win{}
	w.Win, err = acme.New()
	if err != nil {
		u.err(err.Error())
		return nil
	}
	debug("spawning: %q\n", title)
	w.Title = title
	w.Name(path.Join(u.prefix, title))
	w.Ctl("mark")
	w.Ctl("clean")
	u.win[title] = w
	go w.loop(u)
	return w
}

// show forces a window to the top and returns it, if it's found.
func (u *UI) show(title string) *win {
	u.Lock()
	defer u.Unlock()
	if w, ok := u.win[title]; ok {
		debug("showing: %q\n", title)
		w.Ctl("show")
		return w
	}
	return nil
}

// look is show-or-create.
//
// It understands a few magic strings to facilitate this.
func (u *UI) look(title string) bool {
	title = strings.TrimPrefix(title, u.prefix)
	debug("looking: %q\n", title)
	switch title {
	case "my-issues", "mine", "Mine", "", "/":
		if w := u.show("my-issues"); w == nil {
			w = u.new("my-issues")
			if w == nil {
				return false
			}
			w.Ctl("cleartag")
			w.Fprintf("tag", " Get New Search ")
			w.reload = u.fetchMine
			w.reload(w)
		}
		return true
	case "Projects", "Issues", "Search":
		u.err(fmt.Sprintf("%q not implemented yet\n", title))
	}
	if u.projRe.MatchString(title) {
		if w := u.show(title); w == nil {
			// open the issue
			w = u.new(title)

			w.Ctl("cleartag")
			w.Fprintf("tag", issueTag)
			w.reload = u.fetchIssue
			w.put = u.putIssue
			w.Issue = true
			w.reload(w)
		}
		return true
	}
	return false
}

func (u *UI) fetchMine(w *win) {
	l, _, err := u.j.Issue.Search(`assignee = currentUser() AND resolution = Unresolved order by updated DESC`, nil)
	if err != nil {
		u.err(err.Error())
		return
	}

	buf := &bytes.Buffer{}
	wr := tabwriter.NewWriter(buf, 4, 4, 1, '\t', 0)
	for _, i := range l {
		fmt.Fprintf(wr, "%s\t%s/%s\t%s\n",
			i.Key,
			i.Fields.Type.Name,
			i.Fields.Status.Name,
			i.Fields.Summary,
		)
	}
	wr.Flush()

	w.Clear()
	w.Write("data", buf.Bytes())
	w.Ctl("clean")
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (u *UI) exit(title string) {
	u.Lock()
	defer u.Unlock()
	delete(u.win, title)
	if len(u.win) == 0 {
		close(u.exited)
	}
}

func (u *UI) leave() {
	u.Lock()
	defer u.Unlock()
	for title, w := range u.win {
		delete(u.win, title)
		w.Del(true)
	}
	close(u.exited)
}
