// Comando no inicia um servidor regional do sistema de alertas.
//
// Cada opção pode vir de flag ou de variável de ambiente (usado no Docker):
//
//	no -id no1 -porta 8081 -peers http://localhost:8082,http://localhost:8083
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"alertas/internal/credibilidade"
	"alertas/internal/no"
)

func env(chave, padrao string) string {
	if v := os.Getenv(chave); v != "" {
		return v
	}
	return padrao
}

func envNum(chave string, padrao float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(chave), 64); err == nil {
		return v
	}
	return padrao
}

func main() {
	id := flag.String("id", env("NO_ID", "no1"), "identificador do nó (NO_ID)")
	porta := flag.String("porta", env("PORTA", "8081"), "porta HTTP (PORTA)")
	url := flag.String("url", env("URL", ""), "URL deste nó para os outros, ex. http://no1:8080; vazio usa http://localhost:<porta> (URL)")
	peers := flag.String("peers", env("PEERS", ""), "URLs de nós já conhecidos, separadas por vírgula; os outros são descobertos pelo gossip (PEERS)")
	intervalo := flag.Duration("intervalo", time.Duration(envNum("INTERVALO_MS", 1000))*time.Millisecond, "intervalo entre rodadas de gossip")
	fanout := flag.Int("fanout", int(envNum("FANOUT", 2)), "peers contatados por rodada (FANOUT)")
	limiar := flag.Float64("limiar", envNum("LIMIAR_MM_H", 25), "mm/h a partir do qual uma estação dispara alerta sozinha (LIMIAR_MM_H)")
	caminhoModelo := flag.String("modelo", env("MODELO", ""), "JSON do modelo treinado; vazio usa os pesos iniciais (MODELO)")
	flag.Parse()

	modelo := credibilidade.Padrao()
	if *caminhoModelo != "" {
		m, err := credibilidade.Carregar(*caminhoModelo)
		if err != nil {
			log.Fatal(err)
		}
		modelo = m
	}

	var listaPeers []string
	for _, p := range strings.Split(*peers, ",") {
		if p = strings.TrimSpace(p); p != "" {
			listaPeers = append(listaPeers, strings.TrimRight(p, "/"))
		}
	}

	if *url == "" {
		*url = "http://localhost:" + *porta
	}

	n := no.Novo(no.Config{
		ID:           *id,
		URL:          strings.TrimRight(*url, "/"),
		Peers:        listaPeers,
		Intervalo:    *intervalo,
		Fanout:       *fanout,
		Modelo:       modelo,
		LimiarSensor: *limiar,
	})

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()
	go n.Executar(ctx)

	srv := &http.Server{Addr: ":" + *porta, Handler: n.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("[%s] ouvindo em :%s | peers %v | modelo %s", *id, *porta, listaPeers, modelo.Versao)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
