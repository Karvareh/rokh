package shell

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The folder is chosen by the person, with the tool their system already
// gives them for choosing folders. On a Mac that is the Finder's own dialog;
// on a Linux desktop it is whichever folder dialog is installed; and where
// there is none — over ssh, on a server, in a terminal with no desktop behind
// it — the person types the path. All three end the same way: a folder the
// person named, or nothing.
//
// Nothing here reads the folder. Choosing is not opening.

// errNothingChosen is the person closing the dialog, or answering with an
// empty line: they changed their mind, and nothing happens.
var errNothingChosen = errors.New("nothing chosen; nothing opened. Run rokh again, or rokh FOLDER to name the folder.")

// pickerPrompt is the one sentence every dialog carries.
const pickerPrompt = "Choose the folder that is your Rokh, or an empty one to make it in"

// chooseFolder asks the person for a folder and returns its absolute path.
func chooseFolder() (string, error) {
	if cmd := dialog(); cmd != nil {
		out, err := cmd.Output()
		path := strings.TrimSpace(string(out))
		switch {
		case err == nil && path != "":
			return absolute(path)
		case err == nil, isCancel(err):
			// The dialog opened and the person closed it. That is an answer.
			return "", errNothingChosen
		}
		// The dialog could not open at all — no display, no session. Fall
		// through to asking on the terminal; the tool's own words are not the
		// person's problem.
	}
	path, err := askVisible("folder: ")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", errNothingChosen
	}
	return absolute(strings.TrimSpace(path))
}

// dialog is the system's own folder chooser, or nil where there is none to
// try. Only the tool is picked here; running it is chooseFolder's.
func dialog() *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		if osascript, err := exec.LookPath("osascript"); err == nil {
			// Cancel is error -128 in AppleScript; it is caught and answered
			// with an empty line, so that closing the dialog and a folder
			// with no name cannot be told apart — both are "nothing".
			return exec.Command(osascript,
				"-e", "try",
				"-e", `set f to choose folder with prompt "`+pickerPrompt+`"`,
				"-e", "POSIX path of f",
				"-e", "on error number -128",
				"-e", `""`,
				"-e", "end try")
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
			return nil // no desktop to draw a dialog on
		}
		// The desktop's own chooser first, where the desktop says which it
		// is. A person picking a folder should be looking at the file dialog
		// they already know, not at whichever toolkit happened to be
		// installed; and either of these opens the same folder.
		choosers := []string{"zenity", "kdialog"}
		if strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") {
			choosers = []string{"kdialog", "zenity"}
		}
		for _, chooser := range choosers {
			path, err := exec.LookPath(chooser)
			if err != nil {
				continue
			}
			if chooser == "kdialog" {
				home, _ := os.UserHomeDir()
				return exec.Command(path, "--title", pickerPrompt, "--getexistingdirectory", home)
			}
			return exec.Command(path, "--file-selection", "--directory", "--title="+pickerPrompt)
		}
	}
	return nil
}

// isCancel is the exit status the folder dialogs use for "closed without
// choosing": one. Anything else is the tool failing to run, which is not the
// person's answer and is not treated as one.
func isCancel(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// absolute cleans a chosen path: a leading ~ is the person's home, a relative
// path is from where they stand, and the result is absolute so that what the
// screen shows as the vault is what a second terminal would also find.
func absolute(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}
