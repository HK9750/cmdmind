package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/hasnain/cmdmind/internal/suggest"
)

func Pick(menu io.Writer, suggestions []suggest.Suggestion) (string, error) {
	if len(suggestions) == 0 {
		_, _ = fmt.Fprintln(menu, "CmdMind: no suggestions")
		return "", nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		defer tty.Close()
		menu = tty
		return pickFrom(menu, tty, suggestions)
	}

	return pickFrom(menu, os.Stdin, suggestions)
}

func pickFrom(menu io.Writer, input io.Reader, suggestions []suggest.Suggestion) (string, error) {
	_, _ = fmt.Fprintln(menu, "CmdMind suggestions:")
	for i, s := range suggestions {
		marker := " "
		if i == 0 {
			marker = ">"
		}
		_, _ = fmt.Fprintf(menu, "%s %d. %s", marker, i+1, s.CommandText)
		if s.Reason != "" {
			_, _ = fmt.Fprintf(menu, "  (%s)", s.Reason)
		}
		_, _ = fmt.Fprintln(menu)
	}
	_, _ = fmt.Fprint(menu, "Select number, Enter for top, or q to cancel: ")

	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return suggestions[0].CommandText, nil
	}
	if strings.EqualFold(choice, "q") || strings.EqualFold(choice, "esc") {
		return "", nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(suggestions) {
		return "", nil
	}
	return suggestions[n-1].CommandText, nil
}
