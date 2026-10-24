package no

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"alertas/internal/alerta"
)

// Uma rodada de anti-entropia push-pull (Demers et al., 1987) entre A e B:
//
//  1. A envia seu Digest: hash de cada alerta e de cada conjunto de leituras
//     por sensor.
//  2. B compara com o próprio estado e responde com tudo o que A não tem ou
//     tem diferente, mais a lista do que B quer de A.
//  3. A mescla o que recebeu e envia o que B pediu (já mesclado).
//
// No fim da rodada A e B têm o mesmo estado. Como o merge é um CRDT, não
// importa a ordem das rodadas nem se mensagens se repetem.

type Digest struct {
	De       string            `json:"de"`
	Alertas  map[string]string `json:"alertas"`
	Sensores map[string]string `json:"sensores"`
}

type Pacote struct {
	De       string           `json:"de"`
	Alertas  []*alerta.Alerta `json:"alertas,omitempty"`
	Leituras []alerta.Leitura `json:"leituras,omitempty"`
}

type RespostaSync struct {
	Pacote
	QueroAlertas  []string `json:"quero_alertas,omitempty"`
	QueroSensores []string `json:"quero_sensores,omitempty"`
}

func (n *No) leiturasPorSensor() map[string][]alerta.Leitura {
	por := map[string][]alerta.Leitura{}
	for _, l := range n.leituras {
		por[l.Sensor] = append(por[l.Sensor], l)
	}
	return por
}

func (n *No) digest() Digest {
	n.mu.Lock()
	defer n.mu.Unlock()
	d := Digest{De: n.cfg.ID, Alertas: map[string]string{}, Sensores: map[string]string{}}
	for id, a := range n.alertas {
		d.Alertas[id] = a.Hash()
	}
	for s, ls := range n.leiturasPorSensor() {
		d.Sensores[s] = alerta.HashLeituras(ls)
	}
	return d
}

// responderSync é o passo 2, executado por B.
func (n *No) responderSync(d Digest) RespostaSync {
	n.mu.Lock()
	defer n.mu.Unlock()
	r := RespostaSync{Pacote: Pacote{De: n.cfg.ID}}

	for id, a := range n.alertas {
		if h, ok := d.Alertas[id]; !ok || h != a.Hash() {
			r.Alertas = append(r.Alertas, a.Copia())
		}
	}
	for id, h := range d.Alertas {
		if a, ok := n.alertas[id]; !ok || a.Hash() != h {
			r.QueroAlertas = append(r.QueroAlertas, id)
		}
	}

	por := n.leiturasPorSensor()
	for s, ls := range por {
		if h, ok := d.Sensores[s]; !ok || h != alerta.HashLeituras(ls) {
			r.Leituras = append(r.Leituras, ls...)
		}
	}
	for s, h := range d.Sensores {
		if ls, ok := por[s]; !ok || alerta.HashLeituras(ls) != h {
			r.QueroSensores = append(r.QueroSensores, s)
		}
	}
	return r
}

// aplicar mescla um pacote recebido e conta o que mudou no estado local.
func (n *No) aplicar(p Pacote) (alertas, leituras int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, a := range p.Alertas {
		if a != nil && n.mesclarAlerta(a) {
			alertas++
		}
	}
	for _, l := range p.Leituras {
		if n.mesclarLeitura(l) {
			leituras++
		}
	}
	return
}

func (n *No) pacoteCom(ids, sensores []string) Pacote {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := Pacote{De: n.cfg.ID}
	for _, id := range ids {
		if a, ok := n.alertas[id]; ok {
			p.Alertas = append(p.Alertas, a.Copia())
		}
	}
	por := n.leiturasPorSensor()
	for _, s := range sensores {
		p.Leituras = append(p.Leituras, por[s]...)
	}
	return p
}

// rodada executa os passos 1 e 3 contra um peer e devolve o ID dele.
func (n *No) rodada(ctx context.Context, peer string) (string, error) {
	var resp RespostaSync
	if err := n.postar(ctx, peer+"/gossip/sync", n.digest(), &resp); err != nil {
		return "", err
	}
	ra, rl := n.aplicar(resp.Pacote)

	envio := n.pacoteCom(resp.QueroAlertas, resp.QueroSensores)
	if len(envio.Alertas)+len(envio.Leituras) > 0 {
		if err := n.postar(ctx, peer+"/gossip/push", envio, nil); err != nil {
			return resp.De, err
		}
	}
	if ra+rl+len(envio.Alertas)+len(envio.Leituras) > 0 {
		var partes []string
		if ra+rl > 0 {
			partes = append(partes, "recebeu "+contagem(ra, rl))
		}
		if len(envio.Alertas)+len(envio.Leituras) > 0 {
			partes = append(partes, "enviou "+contagem(len(envio.Alertas), len(envio.Leituras)))
		}
		n.registrar(false, "Sincronizado com %s (gossip): %s.", resp.De, strings.Join(partes, "; "))
	}
	return resp.De, nil
}

func (n *No) postar(ctx context.Context, url string, corpo, saida any) error {
	b, err := json.Marshal(corpo)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := n.cliente.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	if saida == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(saida)
}

func (n *No) escolherPeers() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	urls := make([]string, 0, len(n.peers))
	for u := range n.peers {
		urls = append(urls, u)
	}
	rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
	return urls[:min(n.cfg.Fanout, len(urls))]
}

func (n *No) marcarPeer(url, id string, err error) {
	n.mu.Lock()
	p, ok := n.peers[url]
	if !ok {
		n.mu.Unlock()
		return
	}
	nome := p.ID
	if nome == "" {
		nome = url
	}
	falhasAntes := p.Falhas
	if err != nil {
		p.Falhas++
	} else {
		p.Falhas = 0
		p.UltimoOK = time.Now()
		p.ID = id
	}
	n.mu.Unlock()

	// só registra as transições, para não encher o log a cada rodada
	switch {
	case err != nil && falhasAntes == 0:
		n.registrar(true, "%s não respondeu; parece estar fora do ar. Os alertas continuam funcionando aqui e serão repassados quando ele voltar. [%v]", nome, err)
	case err == nil && falhasAntes > 0:
		n.registrar(false, "%s voltou a responder após %s sem sucesso e vai receber o que perdeu.", id, plural(falhasAntes, "tentativa", "tentativas"))
	}
}

func (n *No) rodadaGeral(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range n.escolherPeers() {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			id, err := n.rodada(ctx, p)
			n.marcarPeer(p, id, err)
		}(p)
	}
	wg.Wait()
}

// Executar roda o gossip até o contexto ser cancelado: uma rodada a cada
// intervalo, ou antes disso quando chega algo novo.
func (n *No) Executar(ctx context.Context) {
	t := time.NewTicker(n.cfg.Intervalo)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-n.gatilho:
		}
		n.rodadaGeral(ctx)
		n.podar()
	}
}

// contagem descreve um lote de gossip para o log, ex.: "1 alerta e 3 leituras".
func contagem(alertas, leituras int) string {
	switch {
	case alertas > 0 && leituras > 0:
		return plural(alertas, "alerta", "alertas") + " e " + plural(leituras, "leitura", "leituras")
	case alertas > 0:
		return plural(alertas, "alerta", "alertas")
	case leituras > 0:
		return plural(leituras, "leitura", "leituras")
	}
	return "nada"
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return "1 " + um
	}
	return fmt.Sprintf("%d %s", n, varios)
}
