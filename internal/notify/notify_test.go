package notify

import "testing"

func TestLogNotifier_RecordsAndReportsLast(t *testing.T) {
	n := NewLogNotifier()
	if _, ok := n.Last(); ok {
		t.Fatal("Last() should be false on an empty notifier")
	}
	msgs := []Message{
		{To: "a@example.com", Subject: "one", Body: "code 111111"},
		{To: "b@example.com", Subject: "two", Body: "code 222222"},
	}
	for _, m := range msgs {
		if err := n.Send(m); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	got := n.Sent()
	if len(got) != 2 {
		t.Fatalf("Sent() len = %d, want 2", len(got))
	}
	last, ok := n.Last()
	if !ok || last.To != "b@example.com" || last.Body != "code 222222" {
		t.Fatalf("Last() = %+v, ok=%v; want the second message", last, ok)
	}
}

func TestBuildRFC5322_HasHeadersAndBody(t *testing.T) {
	payload := string(buildRFC5322("from@example.com", Message{
		To:      "to@example.com",
		Subject: "hello",
		Body:    "world",
	}))
	for _, want := range []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		"Subject: hello\r\n",
		"\r\nworld\r\n",
	} {
		if !contains(payload, want) {
			t.Errorf("payload missing %q\n---\n%s", want, payload)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
