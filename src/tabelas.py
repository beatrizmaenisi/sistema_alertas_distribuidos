import pandas as pd

import config


def tabela_qualidade(resultados):
    return pd.DataFrame([{
        "Cenário": r["id"],
        "Tipo": r["tipo"],
        "Linhas brutas": r["linhas_brutas"],
        "Estações brutas": r["estacoes_brutas"],
        "Estações excluídas": r["n_excluidas"],
        "Estações usadas": r["estacoes_usadas"],
        "Linhas após filtro": r["linhas_filtradas"],
        "Intervalo típico (min)": round(r["intervalo_tipico_min"], 1),
        "Buracos": r["buracos"],
        "Buracos (%)": round(r["pct_buracos"], 2),
    } for r in resultados])


def tabela_estatisticas(resultados):
    return pd.DataFrame([{
        "Cenário": r["id"],
        "Média (mm)": round(r["stats"]["media"], 4),
        "Desvio padrão": round(r["stats"]["desvio"], 4),
        "Máximo (mm)": r["stats"]["maximo"],
        "Leituras": int(r["stats"]["n"]),
    } for r in resultados])


def tabela_janelas(resultados):
    return pd.DataFrame([{
        "Cenário": r["id"],
        "Início (UTC)": r["janela_inicio"],
        "Fim (UTC)": r["janela_fim"],
        "mm por estação": round(r["chuva_janela"]["media_por_estacao"], 1),
        "Soma (mm)": round(r["chuva_janela"]["soma"], 2),
        "Silêncios >=3h na janela": r["silencios"]["na_janela"],
        "Taxa na janela (/h)": round(r["silencios"]["taxa_janela_por_h"], 3),
        "Silêncios >=3h fora": r["silencios"]["fora"],
        "Taxa fora (/h)": round(r["silencios"]["taxa_fora_por_h"], 3),
    } for r in resultados])


def tabela_robustez(resultados):
    linhas = []
    for r in resultados:
        linha = {"Cenário": r["id"]}
        for m, v in r["robustez"].items():
            linha[f"Buracos (%) mult. {m}"] = round(v, 2)
        linhas.append(linha)
    return pd.DataFrame(linhas)


def tabela_excluidas(resultados):
    partes = [r["excluidas"] for r in resultados if len(r["excluidas"]) > 0]
    if not partes:
        return pd.DataFrame()
    return pd.concat(partes, ignore_index=True).round({"pct_da_mediana": 1})


def exportar(df, nome):
    config.resultados.mkdir(parents=True, exist_ok=True)
    caminho = config.resultados / f"{nome}.csv"
    # ; e vírgula decimal + utf-8-sig: abre direto no Excel em português
    df.to_csv(caminho, index=False, sep=";", decimal=",", encoding="utf-8-sig")
    return caminho