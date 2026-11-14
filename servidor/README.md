# Alertas Distribuídos - servidor regional

Este diretório contém o servidor regional do sistema de alertas distribuídos. Cada servidor recebe relatos de cidadãos e leituras de pluviômetros IoT, calcula a credibilidade de cada alerta e replica seu estado para os demais servidores por meio de um protocolo *gossip*, sem ponto único de falha.

O documento está organizado em três partes: instalação e execução, arquitetura e justificativa das decisões de projeto.

## Requisitos

| Componente | Versão | Finalidade |
|---|---|---|
| Docker e Docker Compose | recente | Execução dos três nós (modo recomendado) |
| Go | 1.24 ou superior | Compilação local e ferramentas de simulação |
| Compilador C (gcc) | qualquer | Exigido pelo cgo, do qual o [h3-go](https://github.com/uber/h3-go) depende |
| Python | 3.10 ou superior | Apenas para treinar e exportar o modelo de credibilidade |

## Execução

### Com Docker (recomendado)

A partir do diretório `servidor/`:

```sh
docker compose up --build
```

O comando constrói a imagem e inicia os nós `no1`, `no2` e `no3`. Os painéis ficam disponíveis em:

- http://localhost:8081 (`no1`)
- http://localhost:8082 (`no2`)
- http://localhost:8083 (`no3`)

Para observar a tolerância a falhas, basta interromper um nó e reiniciá-lo:

```sh
docker compose stop no2
docker compose start no2
```

Ao retornar, o nó recupera todo o estado perdido na primeira rodada de *gossip*.

### Sem Docker

Com Go e gcc instalados (no Arch Linux, `sudo pacman -S go gcc`):

```sh
go build -o bin/ ./cmd/...   # gera bin/no, bin/replay e bin/relato
./scripts/cluster.sh         # inicia os três nós locais
go test ./...                # executa os testes
```

O `cluster.sh` encerra todos os nós ao receber Ctrl+C. Para derrubar apenas um deles, utiliza-se `kill $(cat /tmp/alertas-no2.pid)`. As portas são as mesmas do modo Docker, de modo que os painéis mantêm o mesmo endereço nos dois casos.

### Configuração do nó

Cada opção pode ser definida por *flag* ou por variável de ambiente; a *flag* tem precedência.

| Flag | Variável | Padrão | Descrição |
|---|---|---|---|
| `-id` | `NO_ID` | `no1` | Identificador do nó |
| `-porta` | `PORTA` | `8081` | Porta HTTP |
| `-peers` | `PEERS` | vazio | URLs dos demais nós, separadas por vírgula |
| `-intervalo` | `INTERVALO_MS` | `1s` | Intervalo entre rodadas de *gossip*. A *flag* recebe uma duração (`500ms`, `2s`); a variável, um inteiro em milissegundos |
| `-fanout` | `FANOUT` | `2` | Número de *peers* contatados por rodada |
| `-limiar` | `LIMIAR_MM_H` | `25` | Intensidade (mm/h) a partir da qual uma estação dispara um alerta sem depender de relatos |
| `-modelo` | `MODELO` | vazio | Arquivo JSON do modelo treinado; se vazio, usam-se os pesos iniciais |

### Ferramentas de simulação

Os comandos abaixo devem ser executados em `servidor/`, com os nós em execução.

**`replay`** reproduz dados reais do CEMADEN como se fossem leituras de sensores IoT. Por padrão, reproduz o evento de Petrópolis de 15/02/2022 a partir de `../dados/raw/RJ_Petropolis_2022-02.csv`:

```sh
./bin/replay -velocidade 60   # 60 = uma hora de dados por minuto
```

Outras opções: `-arquivo` (CSV de entrada), `-inicio` e `-fim` (trecho reproduzido, em UTC) e `-nos` (URLs dos nós).

**`relato`** gera relatos de usuários simulados, cada um proveniente de um dispositivo distinto:

```sh
./bin/relato -no http://localhost:8081 -lat -22.505 -lng -43.18 -n 3 -comentario "rua alagada"
```

Outras opções: `-tipo` (`ALAGAMENTO`, `DESLIZAMENTO`, `CHUVA_FORTE` ou `QUEDA_ARVORE`) e `-espalhar` (raio, em metros, em torno do ponto informado).

### Roteiro de demonstração

1. Abrir os três painéis lado a lado e verificar que todos exibem o mesmo **hash de estado** no topo.
2. Executar `./bin/replay -velocidade 120`. As leituras chegam a nós diferentes, e as estações acima de 25 mm/h disparam alertas por conta própria (sensor como nó âncora). Os três painéis passam a exibir os mesmos alertas.
3. Clicar no mapa do `no1` em uma área sem estação: surge um alerta INFO (score ≈ 0,14 com o modelo `lr-v1`). Clicar próximo ao mesmo ponto no painel do `no3`: o score aumenta nos três nós (corroboração entre servidores).
4. Executar `./bin/relato -no http://localhost:8082 -lat -22.9068 -lng -43.1729 -n 5`, que envia cinco relatos falsos em um local sem chuva. O score sobe, mas o alerta permanece em ATENCAO: sem confirmação de sensor, não atinge EMERGENCIA.
5. Executar `docker compose stop no2`. Os demais nós passam a exibi-lo como OFFLINE; novos relatos continuam sendo aceitos pelo `no1`. Em seguida, `docker compose start no2`: o nó retorna sem estado e, em cerca de 1 s, recupera tudo, voltando a apresentar o mesmo hash.

### Aplicativo móvel (PWA)

O painel de cada nó é também um aplicativo web progressivo: pode ser instalado no celular, abre sem conexão e guarda os relatos feitos offline para enviá-los quando o servidor voltar a responder.

| Arquivo (em `internal/no/web/`) | Papel |
|---|---|
| `manifest.json` e `icones/` | Nome, ícone e cores do aplicativo instalado |
| `sw.js` | *Service worker*: busca cada arquivo na rede e, se ela falhar, usa a cópia guardada (Cache API) |
| `js/fila.js` | Fila de relatos pendentes no aparelho (IndexedDB) |
| `js/painel.js` | Mapa, envio de relatos (`POST /api/relatos`) e reenvio da fila |

O navegador só libera *service worker* e GPS em HTTPS ou em `localhost`. Há duas formas de demonstrar:

- **Simulador:** abrir `http://localhost:8081` no Chrome, pressionar F12 e ativar o modo de dispositivo (Ctrl+Shift+M). O botão **Instalar** aparece no cabeçalho.
- **Celular Android por USB:** com a depuração USB ativada, executar `adb reverse tcp:8081 tcp:8081` e abrir `http://localhost:8081` no Chrome do celular; depois, menu → *Adicionar à tela inicial*.

Para demonstrar o uso offline: ativar o modo avião (ou interromper o nó), tocar em **Relatar onde estou** e observar o aviso "1 relato aguardando conexão"; ao restabelecer a conexão, o relato é enviado com o horário original.

## Arquitetura

```
cmd/no                  servidor regional (API HTTP, gossip e painel)
cmd/replay              reprodução de CSV do CEMADEN como sensores IoT
cmd/relato              gerador de relatos de usuários
internal/alerta         estado replicado e regras de merge (CRDT)
internal/credibilidade  cálculo das features e regressão logística
internal/no             nó: estado, gossip, API e painel/aplicativo móvel (web/)
treino/                 exportação do modelo scikit-learn para JSON; celula.py agrupa relatos como os nós
```

### Estado replicado

- **Identificador do alerta:** célula H3 de resolução 8 concatenada à data no horário de Brasília (ex.: `88a8a3c0e3fffff_20220215`).
- **Relatos:** conjunto indexado pelo hash SHA-256 do identificador do dispositivo. O merge é a união dos conjuntos; havendo dois relatos do mesmo dispositivo, prevalece o mais recente.
- **Leituras:** conjunto indexado pelo par (sensor, data/hora). O merge é a união.
- **Status:** reticulado `ATIVO < RECONHECIDO < RESOLVIDO`; o merge adota o maior valor. `EXPIRADO` não é armazenado, mas derivado: um alerta expira após 6 h sem relatos novos.
- **Resolução:** o campo `resolvido_em` registra o instante da resolução mais recente (merge pelo maior valor).
- **Score, nível, centro e validade não são transmitidos:** cada nó os calcula a partir do estado local.

### Gossip

A cada intervalo (ou imediatamente após receber uma novidade), cada nó seleciona `fanout` *peers* e executa uma rodada de anti-entropia *push-pull*:

1. envia um *digest* contendo o hash de cada alerta e do conjunto de leituras de cada sensor;
2. o *peer* responde com os itens ausentes ou divergentes no remetente e solicita os que lhe faltam;
3. o remetente realiza o merge e envia os itens solicitados.

Ao fim da rodada, os dois nós possuem estados idênticos.

### Credibilidade

A credibilidade é estimada por regressão logística sobre três *features*:

| Feature | Definição |
|---|---|
| `log_n_usuarios` | ln(1 + número de usuários distintos com relatos a até 1 km) |
| `chuva_max_mm_h` | maior intensidade medida por estações a até 5 km, desde 1 h antes do primeiro relato até a expiração do alerta |
| `dispersao_km` | distância média dos relatos ao centro do alerta |

O score resultante define o nível: ≥ 0,7 corresponde a EMERGENCIA, ≥ 0,4 a ATENCAO e valores inferiores a INFO.

Na ausência de modelo treinado, os nós utilizam pesos iniciais (`heuristico-v0`). Para utilizar um modelo treinado:

1. Instalar as dependências: `pip install scikit-learn "h3>=4"`.
2. Treinar um `Pipeline(StandardScaler, LogisticRegression)` com as colunas de `FEATURES`, na ordem definida em `treino/exportar_modelo.py`.
3. Exportar o pipeline com `treino/exportar_modelo.py`, gerando `modelo/modelo.json`.
4. Iniciar os nós com `-modelo modelo/modelo.json`. No Docker, a variável `MODELO` de `docker-compose.yml` já aponta para esse arquivo; após treinar de novo, reconstruir a imagem (`docker compose up --build`), pois o diretório `modelo/` é copiado durante o build.

As definições das *features* estão duplicadas em `internal/credibilidade` (Go) e em `treino/exportar_modelo.py` (Python). Qualquer alteração deve ser feita nos dois locais.

### API

| Método | Rota | Finalidade |
|---|---|---|
| POST | `/api/relatos` | Relato de usuário (Mensagem A) |
| POST | `/api/leituras` | Leitura de sensor, individual ou em lista (rota utilizada pelo ESP32) |
| GET | `/api/alertas?lat=&lng=&raio_km=` | Alertas ativos próximos a um ponto (`todos=1` inclui os expirados) |
| POST | `/api/alertas/{id}/status` | Alteração de status, ex.: `{"status": "RESOLVIDO"}` |
| GET | `/api/estado` | Estado completo exibido pelo painel |
| POST | `/gossip/sync`, `/gossip/push` | Comunicação entre nós |

Exemplo de relato:

```json
{"autor_id": "id-do-aparelho", "tipo": "ALAGAMENTO",
 "geolocalizacao": {"lat": -22.505, "lng": -43.18},
 "comentario": "rua alagada", "timestamp_evento": "2026-10-27T14:00:00-03:00"}
```

O campo `timestamp_evento` é opcional.

Exemplo de leitura:

```json
{"id_sensor": "esp32-01", "lat": -22.505, "lng": -43.18, "chuva_mm_h": 12.4}
```

O campo `datahora` é opcional; na sua ausência (por exemplo, em um sensor sem NTP), o servidor adota o horário de recebimento.

## Limitações conhecidas

- O estado é mantido apenas em memória. Um nó reiniciado volta vazio e se recupera pelo *gossip*, mas, se todos os nós caírem simultaneamente, o estado é perdido. A persistência em PostgreSQL (`../sql/schema.sql`) ainda não foi implementada.
- Não há autenticação entre nós nem entre sensores e nós.
- Relatos em lados opostos da borda entre duas células formam dois alertas distintos no mapa. O score de ambos permanece correto, pois a contagem de usuários considera um raio de 1 km.
- Os blocos do mapa (OpenFreeMap) vêm da internet; sem conexão, o aplicativo mostra apenas as regiões do mapa já visitadas.
