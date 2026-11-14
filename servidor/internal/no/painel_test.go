package no

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alertas/internal/alerta"
)

func pegar(t *testing.T, n *No, rota string) string {
	t.Helper()
	w := httptest.NewRecorder()
	n.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, rota, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d: %s", rota, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func TestPainelMostraODiagramaDoCluster(t *testing.T) {
	n := Novo(Config{ID: "no1", URL: "http://no1:8080", Peers: []string{"http://no2:8080", "http://no3:8080"}})
	n.AdicionarRelato(alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.18})
	n.peers["http://no2:8080"].ID = "no2"
	n.peers["http://no2:8080"].UltimoOK = time.Now()
	n.convergiu(func(p *infoPeer) bool { return p.ID == "no2" })

	pagina := pegar(t, n, "/")
	for _, quer := range []string{"<svg", "viewBox", "<circle", "<line", "no2", "configurado", "iguais em todos"} {
		if !strings.Contains(pagina, quer) {
			t.Errorf("a página não contém %q", quer)
		}
	}
	if c := strings.Count(pagina, "<circle"); c != 3 {
		t.Errorf("esperava 3 círculos (este nó e 2 peers), veio %d", c)
	}
	if !strings.Contains(pegar(t, n, "/painel"), `hx-target="#nos"`) {
		t.Error("a atualização não traz o partial #nos")
	}
}
