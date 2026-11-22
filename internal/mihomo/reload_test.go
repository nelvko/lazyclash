package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmittedReloadWaitsForTheCoreDespiteCallerCancellation(t *testing.T) {
	submitted := make(chan struct{})
	complete := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(submitted)
		<-complete
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		replied, err := (Client{BaseURL: server.URL}).Reload(ctx)
		if !replied {
			t.Error("completed reload treated as uncertain")
		}
		result <- err
	}()
	<-submitted
	cancel()
	close(complete)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
