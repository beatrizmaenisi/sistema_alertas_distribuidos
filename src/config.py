from pathlib import Path

raiz = Path(__file__).resolve().parent.parent

dados_raw = raiz / "dados" / "raw"
dados_processed = raiz / "dados" / "processed"
resultados = raiz / "resultados"

#Estações que não cobrem pelo menos 50% serão excluídas
limiar_cobertura = 50

#Multiplicador de buracos, estações com pelo menos 3 dados ausentes seguidos 
mult_buraco = 3
mult_buraco_teste = [2, 3, 5]

# Duração (em horas) da janela crítica: período contínuo de maior volume de chuva
horas_janela = 6

# Fuso horário: o CEMADEN informa que os dados vêm em UTC.

fuso_arquivo = "UTC"
fuso_local = "America/Sao_Paulo"

# Semente aleatória (será usada no gerador de relatos simulados)
semente = 42

#Cenários
cenarios = [
    {
        "id": "RJ_02",
        "arquivo": "RJ_Petropolis_2022-02.csv",
        "cidade": "Petrópolis",
        "tipo": "crise",
        "janela_inicio": "2022-02-15 18:00:00",
        "janela_fim": "2022-02-16 00:00:00",
        "fuso_janela": "UTC",
        "fonte_janela": "Maior volume em 6 h contínuas (soma das estações), dados CEMADEN. Equivale a 15h-21h no horário local.",
        },

    {
        "id": "RJ_07",
        "arquivo": "RJ_Petropolis_2022-07.csv",
        "cidade": "Petrópolis",
        "tipo": "controle",
        "janela_inicio": "2022-07-29 19:00:00",
        "janela_fim": "2022-07-30 01:00:00",
        "fonte_janela": None,
    },
    {
        "id": "BA_02",
        "arquivo": "BA_Salvador_2022-02.csv",
        "cidade": "Salvador",
        "tipo": "crise",
        "janela_inicio": "2022-02-14 01:00:00",
        "janela_fim": "2022-02-14 07:00:00",
        "fuso_janela": "UTC",
        "fonte_janela": "Maior volume em 6 h contínuas (soma das estações), dados CEMADEN. (Equivale a 22h de 13/02 a 04h de 14/02 no horário local.)",      
    },
    {
        "id": "BA_10",
        "arquivo": "BA_Salvador_2022-10.csv",
        "cidade": "Salvador",
        "tipo": "controle",
        "janela_inicio": "2022-10-25 04:00:00",
        "janela_fim": "2022-10-25 10:00:00",
        "fonte_janela": None,
    },
]

# Conexão com o PostgreSQL
pg_usuario = "postgres"
pg_host = "localhost"
pg_porta = 5432

# Um banco por servidor
bancos = ["servidor1", "servidor2", "servidor3"]

# Cada cidade é atendida por um servidor regional
banco_por_cidade = {
    "Petrópolis": "servidor1",
    "Salvador": "servidor2",
}