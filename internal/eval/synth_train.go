package eval

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Hard training questions (decision 0123): paraphrased and unanswerable
// questions in the training split, in about the proportions of the
// evaluated split (22% paraphrase, 11% unanswerable), so that whatever is
// calibrated or learned on the training split sees the same kinds of
// question it is evaluated on.
//
// They are drawn from their own stream (seed + "/hard-training") and
// appended after every other question, so the corpus, the evaluated and gate
// questions and the earlier training questions (ids t001..t099) are
// byte-identical to the generator without them. The wordings are new: none
// repeats an evaluated or gate question; the paraphrases target the same
// kinds of evidence as the evaluated paraphrase tag (who is responsible for
// a task, which branch serves a sector, what an item described by its
// features costs, a procedure rule), with little lexical overlap with it.

// HardTrainingSuffix names the stream of the hard training questions.
const HardTrainingSuffix = "/hard-training"

// trainRespQ paraphrases, for the training split, one responsibility per
// key (the resp text of departments) three per department.
var trainRespQ = map[string]string{
	"a conciliação bancária e o fechamento do caixa diário":  "Com quem verifico se o saldo da conta corrente bateu ontem à noite?",
	"o reembolso de despesas de viagem dos colaboradores":    "A quem peço o ressarcimento dos gastos com passagem e hospedagem de uma visita a cliente?",
	"a cobrança de clientes inadimplentes":                   "Quem procura os compradores que deixaram faturas atrasadas sem quitar?",
	"as propostas para grandes contas e licitações públicas": "Quem monta a papelada quando participamos de um pregão de um órgão estatal?",
	"a definição de descontos e da política comercial":       "Com quem negocio uma redução especial no preço para fechar uma venda grande?",
	"o programa de parceiros revendedores":                   "Quem atende os distribuidores que comercializam a plataforma em nosso nome?",
	"a base de conhecimento e os artigos de ajuda":           "Quem redige os manuais de autoatendimento publicados no site?",
	"a escala de plantão de fim de semana":                   "Quem monta o rodízio de quem trabalha nos dias de folga?",
	"o treinamento de novos atendentes":                      "Quem prepara os operadores recém-admitidos da central de chamados?",
	"a revisão de contratos com fornecedores":                "Quem analisa as cláusulas antes de fecharmos com uma empresa terceirizada?",
	"os processos trabalhistas":                              "Quem defende a empresa quando um antigo empregado entra na Justiça?",
	"a proteção de dados pessoais e os pedidos de titulares": "Quem trata as questões de privacidade e de LGPD na empresa?",
	"o plano de saúde e os benefícios":                       "Quem resolve a inclusão da minha esposa na assistência médica e odontológica?",
	"a folha de pagamento e o cálculo de horas extras":       "Quem gera os contracheques e paga o serviço feito além do expediente?",
	"a integração de novos colaboradores":                    "Quem organiza as boas-vindas de quem acabou de ser contratado?",
	"a frota de veículos e o controle de combustível":        "Com quem trato sobre os automóveis corporativos e a gasolina?",
	"as compras de equipamentos e mobiliário":                "Quem encomenda computadores e móveis novos para as salas?",
	"a segurança patrimonial e o controle de acesso":         "Quem coordena os vigias e as câmeras do prédio?",
	"a infraestrutura em nuvem e os servidores de produção":  "Quem opera o ambiente hospedado na AWS onde a aplicação fica disponível?",
	"a segurança da informação e a resposta a incidentes":    "Para quem reporto uma mensagem suspeita com vírus ou golpe?",
	"a gestão de acessos e senhas dos sistemas internos":     "Quem desbloqueia meu usuário quando esqueço a credencial?",
}

// trainSpecialtyQ paraphrases the specialty of a branch office.
var trainSpecialtyQ = map[string]string{
	"atendimento a cooperativas e produtores do agronegócio":   "Em qual cidade e endereço fica o time voltado para fazendeiros e para o setor agrícola?",
	"suporte a redes de varejo e supermercados":                "Onde está sediada a equipe que trabalha com lojistas e hipermercados?",
	"projetos com prefeituras e órgãos públicos":               "Qual a localização do pessoal que faz contratos com o setor público?",
	"implantação em hospitais e clínicas":                      "Onde fica a turma que leva a plataforma para a área hospitalar?",
	"cursos presenciais para clientes":                         "Em que local acontecem as aulas de capacitação com instrutor?",
	"contas de escolas e universidades":                        "Onde trabalha a equipe que atende colégios e faculdades?",
	"operações com transportadoras e operadores logísticos":    "Qual a sede do time que cuida das empresas de caminhões e entregas?",
	"clientes do mercado financeiro e cooperativas de crédito": "Onde fica o escritório voltado a instituições bancárias?",
}

// trainProductQ describes a product by its features instead of its name.
var trainProductQ = map[string]string{
	"Plano Essencial":                    "Quanto custa por pessoa a assinatura mais simples, que admite no máximo cinco contas?",
	"Plano Profissional":                 "Qual a mensalidade por pessoa da opção com gente ilimitada e ajuda por ligação?",
	"Plano Empresarial":                  "Quanto pago por pessoa no pacote topo de linha com SLA contratual?",
	"Suporte Premium":                    "Quanto custa por mês ter ajuda técnica a qualquer hora do dia ou da noite?",
	"Backup Gerenciado":                  "Qual a mensalidade por TB para salvar os arquivos todo dia e guardar por três meses?",
	"Consultoria de Migração":            "Quanto cobramos por hora para levar os registros de um software velho para a plataforma?",
	"Treinamento In Company":             "Quanto custa mandar um professor ensinar o sistema na sede do comprador?",
	"Implantação Assistida":              "Qual o valor do serviço em que preparamos o ambiente inteiro do contratante na largada?",
	"Licença Adicional de Administrador": "Quanto sai por mês acrescentar mais um supervisor com acesso total?",
	"Armazenamento Extra de 100 GB":      "Qual a mensalidade para ter mais disco para guardar PDFs e planilhas?",
	"Integração com ERP":                 "Quanto se paga uma única vez para sincronizar com o software administrativo do comprador?",
	"Módulo de Relatórios Avançados":     "Qual o valor do adicional com gráficos sob encomenda e envio automático de métricas?",
}

// trainRuleQ paraphrases a procedure rule (keyed by its plain question).
var trainRuleQ = map[string]string{
	"Qual é o prazo para pedir reembolso de uma despesa?":                  "Depois de pagar algo do trabalho com meu cartão, até quando consigo pegar o valor de volta?",
	"Com quanta antecedência preciso pedir férias?":                        "Quanto tempo antes devo registrar meu período de descanso anual?",
	"Qual o tamanho mínimo de uma senha?":                                  "Quão comprida precisa ser a credencial de acesso aos computadores?",
	"Qual é o auxílio mensal para quem trabalha de casa?":                  "Quanto recebo por mês para pagar a conta de luz e a banda larga em home office?",
	"Quem paga a multa se eu cometer uma infração com o carro da empresa?": "Se o radar me pegar dirigindo o automóvel corporativo, quem arca com o valor?",
}

// genHardTraining appends the paraphrased and unanswerable training
// questions. It reads what the other generators built but draws only from
// its own stream, so nothing they produced moves.
func (g *synth) genHardTraining(seed string) {
	r := &hashRand{seed: seed + HardTrainingSuffix}
	asked := map[string]bool{}
	for _, q := range g.qs {
		asked[Normalize(q.Question)] = true
	}
	train := func(q Question) {
		if asked[Normalize(q.Question)] {
			panic("hard training question repeats an existing one: " + q.Question)
		}
		asked[Normalize(q.Question)] = true
		g.train(q)
	}
	para := func(question string, gold Gold, answer string) {
		train(Question{Question: question, Gold: []Gold{gold}, Answers: []string{answer}, Tags: []string{TagParaphrase}})
	}

	// Paraphrases: 21 responsibilities, 3 branches, 5 products, 4 rules.
	for _, p := range g.people {
		if q, ok := trainRespQ[p.resp]; ok {
			para(q, Gold{DocID: "dir-" + p.slug, Span: p.respSpan()}, p.name)
		}
	}
	for _, o := range shuffled(r, g.offices)[:3] {
		cur := o.addr[2]
		para(trainSpecialtyQ[o.specialty], Gold{DocID: "filiais", Span: "Endereço: " + cur}, cur)
	}
	for _, p := range shuffled(r, g.products)[:5] {
		cur := p.prices[2]
		para(trainProductQ[p.name], Gold{DocID: "precos", Span: priceSpan(cur)}, money(cur))
	}
	var rules []func()
	for _, pr := range g.procs {
		for _, ru := range pr.rules {
			if q, ok := trainRuleQ[ru.question]; ok {
				pr, ru := pr, ru
				rules = append(rules, func() {
					cur := last(ru.values)
					para(q, Gold{DocID: pr.id, Span: ru.goldSpan(cur)}, ru.answer(cur))
				})
			}
		}
	}
	for _, f := range shuffled(r, rules)[:4] {
		f()
	}

	// Unanswerable: 4 people who do not exist (sharing a first or last name
	// with someone who does), 3 attributes the corpus never states, 3
	// products, 2 cities and 5 topics it does not have.
	var all strings.Builder
	for _, it := range g.items {
		all.WriteString(Normalize(ItemText(it)) + "\n")
	}
	text := all.String()
	abstain := func(q string) { train(Question{Question: q, Tags: []string{TagAbstention}}) }
	templates := []string{"Como falo com %s por telefone?", "Em que equipe trabalha %s?", "Qual o cargo atual de %s?", "Qual o endereço de e-mail de %s?"}
	for n := 0; n < 4; {
		p := g.people[r.intn(len(g.people))]
		parts := strings.SplitN(p.name, " ", 2)
		name := parts[0] + " " + pick(r, lastNames)
		if n%2 == 1 {
			name = pick(r, firstNames) + " " + parts[1]
		}
		q := fmt.Sprintf(templates[n], name)
		if strings.Contains(text, Normalize(name)) || asked[Normalize(q)] {
			continue
		}
		abstain(q)
		n++
	}
	for i, p := range shuffled(r, g.people)[:3] {
		abstain(fmt.Sprintf([]string{"Qual é o número do RG de %s?", "Em que cidade nasceu %s?", "Qual o salário de %s?"}[i], p.name))
	}
	for _, q := range []string{
		"Quanto custa o Plano Bronze?",
		"Qual o valor do Módulo de Controle de Estoque?",
		"Quanto custa a Integração com CRM?",
		"Qual o telefone da filial de Natal?",
		"Onde fica o escritório de Vitória?",
		"Qual é o horário da academia do prédio?",
		"Quem é o síndico do condomínio da sede?",
		"Qual o valor do auxílio-creche?",
		"Qual centro de custo uso para despesas do projeto Zênite?",
		"Qual é o prazo para pedir licença-paternidade?",
	} {
		abstain(q)
	}
}

// trainKeys returns the keys of m, sorted (tests check every entry is used).
func trainKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
