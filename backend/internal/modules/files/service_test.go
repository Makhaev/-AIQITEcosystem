package files

import (
	"context"
	"testing"
)

func TestFileRejectsInvalidInputBeforePersistence(t *testing.T) {
	s := Service{}
	for _, in := range []Input{
		{StudentID: 1, Name: "../secret.txt", Data: []byte("text")},
		{StudentID: 1, Name: "bad\n.txt", Data: []byte("text")},
		{StudentID: 1, Name: "empty.txt"},
		{StudentID: 1, Name: "large.txt", Data: make([]byte, MaxSize+1)},
		{StudentID: 1, Name: "active.html", Data: []byte("<!DOCTYPE html><script>alert(1)</script>")},
		{StudentID: 1, Name: "unknown.exe", Data: []byte{0, 1, 2, 3, 4}},
	} {
		if _, err := s.Create(context.Background(), in); err == nil {
			t.Errorf("accepted %s", in.Name)
		}
	}
}
