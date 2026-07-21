package bootstrap_test

// The wizard-only phase and the full-app phase run in the same process on
// the same listener — completing setup must switch which handler serves
// requests without a restart (§6.2). Gateway is that switch.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyphux/glyphux/internal/bootstrap"
)

func handlerReturning(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

func getBody(h http.Handler) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}

func TestGatewayServesInitialHandler(t *testing.T) {
	g := bootstrap.NewGateway(handlerReturning("wizard"))
	if got := getBody(g); got != "wizard" {
		t.Fatalf("get() = %q, want %q", got, "wizard")
	}
}

func TestGatewaySwitchesToNewHandler(t *testing.T) {
	g := bootstrap.NewGateway(handlerReturning("wizard"))
	g.Switch(handlerReturning("full app"))
	if got := getBody(g); got != "full app" {
		t.Fatalf("get() after Switch = %q, want %q", got, "full app")
	}
}

func TestGatewayIsSafeForConcurrentServeAndSwitch(t *testing.T) {
	g := bootstrap.NewGateway(handlerReturning("wizard"))
	done := make(chan struct{})
	go func() {
		for range 200 {
			getBody(g)
		}
		close(done)
	}()
	for range 200 {
		g.Switch(handlerReturning("full app"))
	}
	<-done
}
