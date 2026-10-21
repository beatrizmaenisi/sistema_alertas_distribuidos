import time

import pandas as pd

import carga
import config
import disponibilidade as disp
import estatistica as est
import qualidade
import tabelas


def processar_cenario(c, df_bruto):
    df, excluidas = qualidade.aplicar_filtro_cobertura(df_bruto)
    buracos = disp.detectar_buracos(df)
    ini, fim, _ = est.janela_critica(df)

    # confere com a janela registrada na config (se houver)
    if c["janela_inicio"] is not None:
        esperado = (pd.Timestamp(c["janela_inicio"]), pd.Timestamp(c["janela_fim"]))
        if (ini, fim) != esperado:
            print(f"AVISO {c['id']}: janela calculada {ini} -> {fim} difere da config {esperado}")

    excluidas_tab = excluidas.reset_index().assign(cenario=c["id"])[
        ["cenario", "codEstacao", "nomeEstacao", "leituras", "pct_da_mediana"]
    ]

    return {
        "id": c["id"],
        "tipo": c["tipo"],
        "linhas_brutas": len(df_bruto),
        "estacoes_brutas": df_bruto["codEstacao"].nunique(),
        "n_excluidas": len(excluidas),
        "excluidas": excluidas_tab,
        "estacoes_usadas": df["codEstacao"].nunique(),
        "linhas_filtradas": len(df),
        "intervalo_tipico_min": disp.intervalo_tipico(df).median() / pd.Timedelta(minutes=1),
        "buracos": len(buracos),
        "pct_buracos": disp.taxa_buracos(df, buracos),
        "robustez": disp.taxa_por_multiplicador(df),
        "stats": est.estatisticas_chuva(df),
        "janela_inicio": ini,
        "janela_fim": fim,
        "chuva_janela": est.chuva_na_janela(df, ini, fim),
        "silencios": disp.silencios_iniciados(buracos, df, ini, fim),
    }


def main():
    inicio = time.perf_counter()

    brutas = carga.carregar_todos()
    resultados = [processar_cenario(c, brutas[c["id"]]) for c in config.cenarios]

    saidas = {
        "tabela_a_qualidade": tabelas.tabela_qualidade(resultados),
        "tabela_b_estatisticas": tabelas.tabela_estatisticas(resultados),
        "tabela_c_janelas": tabelas.tabela_janelas(resultados),
        "tabela_d_robustez": tabelas.tabela_robustez(resultados),
        "tabela_e_estacoes_excluidas": tabelas.tabela_excluidas(resultados),
    }

    for nome, tab in saidas.items():
        caminho = tabelas.exportar(tab, nome)
        print(f"\n{nome} -> {caminho}")
        print(tab.to_string(index=False))

    total_linhas = sum(r["linhas_brutas"] for r in resultados)
    duracao = time.perf_counter() - inicio
    print(f"\nPipeline: {total_linhas} linhas em {duracao:.2f} s ({total_linhas / duracao:.0f} linhas/s)")


if __name__ == "__main__":
    main()