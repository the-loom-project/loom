package clip

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Copy(text string) string {
	via := ""
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		if path, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(path, c[1:]...)
			cmd.Stdin = strings.NewReader(text)
			if cmd.Run() == nil {
				via = c[0]
				break
			}
		}
	}
	fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
	if via == "" {
		return "terminal"
	}
	return via
}
