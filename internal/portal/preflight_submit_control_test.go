package portal

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestJobCheckSubmitControlsUseConfirmation(t *testing.T) {
	ps, _ := checkPortal(t)
	w := checkRequest(t, ps, "POST", "/consumer/job/check", jobForm(), true)
	document, err := html.Parse(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	walkJobCheckHTML(document, func(n *html.Node) {
		count += assertJobCheckSubmitControl(t, n)
	})
	if count != 1 {
		t.Fatalf("Submit job controls = %d, want exactly one", count)
	}
}

func assertJobCheckSubmitControl(t *testing.T, n *html.Node) int {
	t.Helper()
	if n.Type != html.ElementNode || (n.Data != "button" && n.Data != "input") {
		return 0
	}
	action := enclosingCheckFormAction(n)
	if action == "/consumer/job/check" && htmlAttribute(n, "formaction") != "" {
		t.Error("editable check form contains a formaction override")
	}
	label := htmlAttribute(n, "value")
	if n.FirstChild != nil {
		label = strings.TrimSpace(n.FirstChild.Data)
	}
	if label != "Submit job" {
		return 0
	}
	if action != "/consumer/job/check/submit" {
		t.Error("Submit job control is outside the checked confirmation form")
	}
	return 1
}

func enclosingCheckFormAction(n *html.Node) string {
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "form" {
			return htmlAttribute(parent, "action")
		}
	}
	return ""
}
