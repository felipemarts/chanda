#!/usr/bin/env python3
"""Portuguese regression suite of the conflict-resolution study: 40 scripted
pairs with the answer fixed by construction (eval/conflicts/PROTOCOL.md,
Part 2). Writes pairs.json. Each pair is two sessions of one user, the
older first; each session has the user's statement and a short assistant
reply (the neighbouring turn shown to the verdict). expected:
  "supersedes"   the newer statement replaces the older (20 updates);
  "no_supersede" both stay valid (20 controls)."""
import json

R = "Entendi, obrigado por avisar."  # neutral assistant reply

U = [  # (id, category, older date, older text, newer date, newer text)
 ("u01", "endereco", "2023-01-10", "Eu moro em São Paulo, num apartamento na Vila Mariana.", "2023-06-02", "Me mudei para o Rio de Janeiro mês passado, agora moro em Botafogo."),
 ("u02", "endereco", "2023-02-03", "Meu escritório fica na Avenida Paulista, número 1000.", "2023-09-14", "O escritório mudou de endereço: agora fica na Rua Augusta, número 250."),
 ("u03", "endereco", "2023-03-20", "A Ana mora em Curitiba com os pais.", "2023-11-05", "A Ana saiu de Curitiba e agora mora sozinha em Florianópolis."),
 ("u04", "cargo", "2023-01-15", "Trabalho como analista de dados na Natura.", "2023-08-01", "Fui promovido: agora sou gerente de dados na Natura."),
 ("u05", "cargo", "2023-02-11", "O Pedro é estagiário no time de marketing.", "2023-10-20", "O Pedro foi efetivado e agora é coordenador do time de marketing."),
 ("u06", "cargo", "2023-04-02", "Sou professora de matemática na Escola Horizonte.", "2023-12-01", "Saí da Escola Horizonte; agora dou aulas de matemática no Colégio Atlas."),
 ("u07", "preco", "2023-01-05", "O Plano Pro custa R$ 49 por mês.", "2023-07-01", "O Plano Pro subiu: agora custa R$ 59 por mês."),
 ("u08", "preco", "2023-03-08", "A mensalidade da academia é R$ 120.", "2023-09-08", "A academia reajustou a mensalidade para R$ 135."),
 ("u09", "preco", "2023-02-17", "O aluguel do apartamento é R$ 2.800 por mês.", "2023-08-17", "Renovei o contrato e o aluguel passou a ser R$ 3.100 por mês."),
 ("u10", "preferencia", "2023-01-22", "Meu café preferido é o expresso sem açúcar.", "2023-07-22", "Mudei de gosto: hoje meu café preferido é o cappuccino com canela."),
 ("u11", "preferencia", "2023-02-02", "Prefiro reuniões de manhã, antes das 10h.", "2023-09-02", "Agora prefiro reuniões à tarde, depois das 14h, porque deixo as manhãs para foco."),
 ("u12", "preferencia", "2023-05-05", "Para viagens a trabalho eu prefiro a Azul.", "2023-11-25", "Troquei de companhia: para viagens a trabalho agora prefiro a LATAM."),
 ("u13", "projeto", "2023-01-09", "O projeto Atlas está em fase de testes.", "2023-04-09", "O projeto Atlas foi lançado em produção na semana passada."),
 ("u14", "projeto", "2023-02-14", "A migração para o Postgres está pausada até março.", "2023-05-14", "Retomamos a migração para o Postgres e ela já está 80% concluída."),
 ("u15", "projeto", "2023-03-01", "O app novo ainda não tem data de lançamento.", "2023-06-30", "O app novo tem lançamento marcado para 15 de agosto."),
 ("u16", "projeto", "2023-06-12", "O contrato com a Acme está em negociação.", "2023-10-12", "O contrato com a Acme foi cancelado; não vamos mais seguir com eles."),
 ("u17", "quantidade", "2023-01-30", "Tenho 3 gatos em casa.", "2023-09-30", "Adotei mais dois, então agora tenho 5 gatos em casa."),
 ("u18", "quantidade", "2023-02-25", "Nosso time de engenharia tem 12 pessoas.", "2023-08-25", "Depois das contratações, o time de engenharia tem 18 pessoas."),
 ("u19", "quantidade", "2023-03-15", "Corro 10 quilômetros por semana.", "2023-07-15", "Aumentei o treino: agora corro 25 quilômetros por semana."),
 ("u20", "quantidade", "2023-04-18", "A loja tem 40 clientes ativos.", "2023-12-18", "A loja chegou a 75 clientes ativos este mês."),
]
C = [
 ("c01", "outra_pessoa", "2023-01-10", "Eu moro em São Paulo, na Vila Mariana.", "2023-06-02", "Minha irmã mora no Rio de Janeiro, em Botafogo."),
 ("c02", "outra_pessoa", "2023-02-11", "Eu trabalho como analista de dados na Natura.", "2023-08-01", "O Marcos trabalha como gerente de dados na Natura."),
 ("c03", "outra_pessoa", "2023-03-03", "Tenho 3 gatos em casa.", "2023-09-03", "A Júlia tem 5 gatos em casa."),
 ("c04", "outra_pessoa", "2023-04-04", "Meu café preferido é o expresso sem açúcar.", "2023-10-04", "O café preferido do Rafael é o cappuccino com canela."),
 ("c05", "outro_atributo", "2023-01-05", "O Plano Pro custa R$ 49 por mês.", "2023-07-01", "O Plano Pro inclui 50 GB de armazenamento."),
 ("c06", "outro_atributo", "2023-02-06", "O aluguel do apartamento é R$ 2.800 por mês.", "2023-08-06", "O condomínio do apartamento é R$ 650 por mês."),
 ("c07", "outro_atributo", "2023-03-07", "Nosso time de engenharia tem 12 pessoas.", "2023-09-07", "Nosso time de engenharia tem 2 gerentes."),
 ("c08", "soma", "2023-01-08", "Eu falo inglês fluente.", "2023-07-08", "Também falo espanhol, comecei a estudar há dois anos."),
 ("c09", "soma", "2023-02-09", "Gosto de correr no Parque Ibirapuera.", "2023-08-09", "Também gosto de pedalar no Parque Villa-Lobos aos domingos."),
 ("c10", "soma", "2023-03-10", "Tenho um cachorro chamado Thor.", "2023-09-10", "Além do Thor, agora também tenho uma gata chamada Luna."),
 ("c11", "soma", "2023-04-11", "O projeto Atlas usa Go no backend.", "2023-10-11", "O projeto Atlas também usa Python nos scripts de dados."),
 ("c12", "historico", "2023-01-12", "Em 2019 eu morei em Lisboa por seis meses.", "2023-07-12", "Em 2021 eu morei em Berlim por um ano."),
 ("c13", "historico", "2023-02-13", "Meu primeiro emprego foi na padaria do meu tio, em 2010.", "2023-08-13", "Meu segundo emprego foi numa livraria, em 2012."),
 ("c14", "historico", "2023-03-14", "Em março visitei o Chile com a família.", "2023-09-14", "Em agosto visitei o Peru com a família."),
 ("c15", "hipotese", "2023-01-15", "Eu moro em São Paulo, na Vila Mariana.", "2023-07-15", "Estou pensando em me mudar para o Rio de Janeiro no ano que vem, mas ainda não decidi."),
 ("c16", "hipotese", "2023-02-16", "O Plano Pro custa R$ 49 por mês.", "2023-08-16", "Se o Plano Pro passar de R$ 70 por mês, vou cancelar a assinatura."),
 ("c17", "hipotese", "2023-03-17", "Trabalho como analista de dados na Natura.", "2023-09-17", "Talvez eu me candidate a uma vaga de gerente na Ambev em dezembro."),
 ("c18", "citacao", "2023-01-18", "Eu moro em São Paulo, na Vila Mariana.", "2023-07-18", "Meu chefe disse brincando: \"você devia morar no Rio de Janeiro\"."),
 ("c19", "citacao", "2023-02-19", "Tenho 3 gatos em casa.", "2023-08-19", "A veterinária comentou que \"quem tem 5 gatos precisa de duas caixas de areia a mais\"."),
 ("c20", "citacao", "2023-03-20", "Meu café preferido é o expresso sem açúcar.", "2023-09-20", "Minha mãe sempre diz: \"o melhor café é o cappuccino com canela\"."),
]

def rec(date, text):
    return {"date": date + "T10:00:00Z", "turns": [{"role": "user", "content": text}, {"role": "assistant", "content": R}]}

pairs = [{"id": i, "kind": "update", "category": c, "older": rec(d1, t1), "newer": rec(d2, t2), "expected": "supersedes"} for i, c, d1, t1, d2, t2 in U]
pairs += [{"id": i, "kind": "control", "category": c, "older": rec(d1, t1), "newer": rec(d2, t2), "expected": "no_supersede"} for i, c, d1, t1, d2, t2 in C]
assert len(pairs) == 40 and len({p["id"] for p in pairs}) == 40
json.dump({"language": "pt-BR", "pairs": pairs}, open(__file__.rsplit("/", 1)[0] + "/pairs.json", "w"), ensure_ascii=False, indent=1)
print("wrote pairs.json: 40 pairs")
