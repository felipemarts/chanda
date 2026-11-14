package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SyntheticSeed is the seed of the committed eval/data/synthetic-pt set.
const SyntheticSeed = "chanda-synthetic-pt-v2"

// TagParaphrase marks questions worded with little lexical overlap with
// their evidence (synonyms, descriptions instead of names).
const TagParaphrase = "paraphrase"

// hashRand is a deterministic stream: SHA-256(seed "/" counter). It replaces
// math/rand so the corpus is identical on every platform and Go version.
type hashRand struct {
	seed string
	n    uint64
}

func (r *hashRand) next() uint64 {
	r.n++
	h := sha256.Sum256([]byte(r.seed + "/" + strconv.FormatUint(r.n, 10)))
	return binary.BigEndian.Uint64(h[:8])
}

func (r *hashRand) intn(n int) int { return int(r.next() % uint64(n)) }

// between returns an int in [lo, hi].
func (r *hashRand) between(lo, hi int) int { return lo + r.intn(hi-lo+1) }

func pick[T any](r *hashRand, xs []T) T { return xs[r.intn(len(xs))] }

func shuffled[T any](r *hashRand, xs []T) []T {
	out := append([]T(nil), xs...)
	for i := len(out) - 1; i > 0; i-- {
		j := r.intn(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// money formats cents as Brazilian reais: R$ 1.234,90.
func money(cents int) string {
	reais := strconv.Itoa(cents / 100)
	var b strings.Builder
	for i, c := range reais {
		if i > 0 && (len(reais)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("R$ %s,%02d", b.String(), cents%100)
}

// brDate turns YYYY-MM-DD into DD/MM/YYYY.
func brDate(d string) string { return d[8:10] + "/" + d[5:7] + "/" + d[0:4] }

const (
	company  = "Aurora Serviços Digitais"
	domain   = "aurora.example"
	synthNow = "2026-11-01" // as_of of "current" questions
)

type synth struct {
	r       *hashRand
	items   []Item
	qs      []Question
	nEval   int
	nTrain  int
	nGate   int
	version map[string]int
	ramals  map[int]bool
	cents   map[int]bool
	names   map[string]bool

	people   []*person
	offices  []*office
	products []*product
	procs    []*procedure
}

func (g *synth) add(it Item) {
	if it.Type == ItemDocument {
		g.version[it.ID]++
		it.Version = g.version[it.ID]
	}
	g.items = append(g.items, it)
}

func (g *synth) doc(id, uri, date, title, text string) {
	g.add(Item{Type: ItemDocument, ID: id, URI: uri, Date: date, Source: "document", Title: title, Text: text})
}

func (g *synth) external(id, uri, date, source, title, text string) {
	g.add(Item{Type: ItemDocument, ID: id, URI: uri, Date: date, Source: source, Title: title, Text: text})
}

func (g *synth) session(id, date string, facts []Statement, turns ...Turn) {
	g.add(Item{Type: ItemSession, ID: id, URI: "session://aurora/" + id, Date: date, Source: "session", Turns: turns, Facts: facts})
}

func (g *synth) ask(q Question) {
	if q.AsOf == "" {
		q.AsOf = synthNow
	}
	switch q.Split {
	case SplitTrain:
		g.nTrain++
		q.ID = fmt.Sprintf("t%03d", g.nTrain)
	case SplitGate:
		g.nGate++
		q.ID = fmt.Sprintf("g%03d", g.nGate)
	default:
		g.nEval++
		q.ID = fmt.Sprintf("q%03d", g.nEval)
	}
	sort.Strings(q.Tags)
	g.qs = append(g.qs, q)
}

func (g *synth) train(q Question) {
	q.Split = SplitTrain
	g.ask(q)
}

// gate adds a question to the gate split: what C5 approves as its gate set.
func (g *synth) gate(q Question) {
	q.Split = SplitGate
	g.ask(q)
}

func (g *synth) ramal() int {
	for {
		n := g.r.between(2000, 8999)
		if !g.ramals[n] {
			g.ramals[n] = true
			return n
		}
	}
}

// price returns unique cents in [lo, hi] reais ending in ,00 ,50 or ,90.
func (g *synth) price(lo, hi int) int {
	for {
		c := g.r.between(lo, hi)*100 + pick(g.r, []int{0, 50, 90})
		if !g.cents[c] {
			g.cents[c] = true
			return c
		}
	}
}

// bump returns a new unique price 5% to 25% above old.
func (g *synth) bump(old int) int {
	for {
		c := (old*g.r.between(105, 125)/100)/10*10 + pick(g.r, []int{0, 0, 9})
		if !g.cents[c] && c != old {
			g.cents[c] = true
			return c
		}
	}
}

func userTurn(s string) Turn { return Turn{Role: "user", Text: s} }
func botTurn(s string) Turn  { return Turn{Role: "assistant", Text: s} }
func toolTurn(name, s string) Turn {
	return Turn{Role: "tool", Name: name, Text: s}
}

// last returns the final element of xs.
func last[T any](xs []T) T { return xs[len(xs)-1] }

// distinct returns the values of xs that differ from keep, in order, once.
func distinct[T comparable](xs []T, keep T) []T {
	var out []T
	seen := map[T]bool{keep: true}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// GenerateSyntheticPT builds the synthetic Portuguese company corpus. It is
// built to be hard for retrieval and to exercise every principle:
//
//   - questions paraphrased with little lexical overlap with their evidence;
//   - many distractor documents and sessions with the same vocabulary
//     (minutes, project reports, branch newsletters, chit-chat);
//   - a corpus far larger than the 800-token budget;
//   - temporal chains: three or more successive values of single predicates
//     (job title, extension, address, price, procedure limits, supplier)
//     across document versions and session statements, with as_of questions;
//   - near-duplicate paraphrases (price notices, FAQ) and contradicting
//     session statements;
//   - instructions injected through web pages, tool results and tool turns;
//   - unanswerable questions, many of them near misses;
//   - a training split (other wordings of the same tasks) for C5 and for
//     calibrating the context cut, with its own paraphrased and unanswerable
//     questions (genHardTraining, decision 0123).
//
// The output depends only on seed.
func GenerateSyntheticPT(seed string) *Dataset {
	g := &synth{r: &hashRand{seed: seed}, version: map[string]int{}, ramals: map[int]bool{},
		cents: map[int]bool{1990: true, 9000000: true}, names: map[string]bool{}}
	g.genOffices()
	g.genDirectory()
	g.genPrices()
	g.genProcedures()
	g.genSessions()
	g.genExternal()
	g.genDistractors()
	g.genAbstention()
	g.genHardTraining(seed)

	sort.SliceStable(g.items, func(i, j int) bool { return g.items[i].Date < g.items[j].Date })
	return &Dataset{
		Manifest: Manifest{
			Name:     "synthetic-pt",
			Language: "pt-BR",
			Description: "Synthetic company corpus (" + company + "): directory, branch offices, prices, procedures, sessions and external content, " +
				"with distractors, paraphrased questions, temporal chains across document versions and session statements, " +
				"near-duplicates, contradicting updates, injected instructions, unanswerable questions and a training split.",
			Generator: "go run ./eval/cmd/gensynth",
			Seed:      seed,
		},
		Corpus:    g.items,
		Questions: g.qs,
	}
}

// ---------------------------------------------------------------------------
// Branch offices: three versions of one document.

type office struct {
	city, ddd, specialty, specialtyQ string
	addr, bairro, cep, phone         []string // per version (3)
	hours                            string
}

var allCities = []struct{ city, ddd string }{
	{"São Paulo", "11"}, {"Curitiba", "41"}, {"Recife", "81"}, {"Belo Horizonte", "31"},
	{"Porto Alegre", "51"}, {"Salvador", "71"}, {"Florianópolis", "48"}, {"Goiânia", "62"},
}

var specialties = [][2]string{
	{"atendimento a cooperativas e produtores do agronegócio", "Onde fica o escritório que cuida das contas de fazendas e cooperativas rurais?"},
	{"suporte a redes de varejo e supermercados", "Qual o endereço da unidade que atende cadeias de lojas e mercados?"},
	{"projetos com prefeituras e órgãos públicos", "Onde trabalha o time que faz projetos para governos municipais?"},
	{"implantação em hospitais e clínicas", "Em que rua fica a base que instala o sistema na área da saúde?"},
	{"cursos presenciais para clientes", "Onde ficam as salas em que os usuários fazem capacitação ao vivo?"},
	{"contas de escolas e universidades", "Qual o endereço do escritório que cuida de instituições de ensino?"},
	{"operações com transportadoras e operadores logísticos", "Onde fica a unidade dedicada a empresas de frete?"},
	{"clientes do mercado financeiro e cooperativas de crédito", "Em que endereço está a equipe que atende bancos?"},
}

var streets = []string{
	"Rua das Palmeiras", "Avenida Paulista", "Rua XV de Novembro", "Avenida Boa Viagem", "Rua da Bahia",
	"Avenida Ipiranga", "Rua Chile", "Avenida Beira-Mar Norte", "Rua Augusta", "Avenida Afonso Pena",
	"Rua Padre Chagas", "Avenida Sete de Setembro", "Rua dos Andradas", "Avenida T-63", "Rua Marechal Deodoro",
	"Rua Voluntários da Pátria", "Avenida Brasil", "Rua Sergipe",
}

var bairros = []string{"Centro", "Jardins", "Boa Viagem", "Savassi", "Moinhos de Vento", "Trindade", "Batel", "Pituba", "Setor Bueno", "Funcionários"}

var officeDates = []string{"2026-01-12", "2026-05-04", "2026-09-14"}

func (g *synth) newAddress(used map[string]bool) (string, string, string) {
	for {
		s := fmt.Sprintf("%s, %d", pick(g.r, streets), g.r.between(100, 2999))
		if !used[s] {
			used[s] = true
			return s, pick(g.r, bairros), fmt.Sprintf("%05d-%03d", g.r.between(10000, 99999), g.r.intn(1000))
		}
	}
}

func (g *synth) phone(ddd string) string {
	return fmt.Sprintf("(%s) 3%03d-%04d", ddd, g.r.intn(1000), g.r.intn(10000))
}

func (g *synth) genOffices() {
	cities := shuffled(g.r, allCities)[:6]
	sort.Slice(cities, func(i, j int) bool { return cities[i].city < cities[j].city })
	specs := shuffled(g.r, specialties)
	used := map[string]bool{}
	for i, c := range cities {
		ad, b, cep := g.newAddress(used)
		h1 := g.r.between(7, 9)
		o := &office{city: c.city, ddd: c.ddd, specialty: specs[i][0], specialtyQ: specs[i][1],
			addr: []string{ad, ad, ad}, bairro: []string{b, b, b}, cep: []string{cep, cep, cep},
			hours: fmt.Sprintf("das %dh às %dh", h1, h1+9)}
		p := g.phone(c.ddd)
		o.phone = []string{p, p, p}
		g.offices = append(g.offices, o)
	}
	order := shuffled(g.r, []int{0, 1, 2, 3, 4, 5})
	move := func(o *office, from int) {
		ad, b, cep := g.newAddress(used)
		for v := from; v < 3; v++ {
			o.addr[v], o.bairro[v], o.cep[v] = ad, b, cep
		}
	}
	move(g.offices[order[0]], 1) // moves twice: a three-value chain
	move(g.offices[order[0]], 2)
	move(g.offices[order[1]], 1)
	move(g.offices[order[2]], 2)
	for _, i := range order[3:5] {
		g.offices[i].phone[2] = g.phone(g.offices[i].ddd)
	}
	render := func(v int) string {
		var b strings.Builder
		fmt.Fprintf(&b, "# Endereços das filiais\n\nEndereços, telefones e especialidades das filiais da %s. Atualizado em %s.\n", company, brDate(officeDates[v]))
		for _, o := range g.offices {
			fmt.Fprintf(&b, "\n## %s\n\nEndereço: %s\nBairro: %s\nCEP: %s\nTelefone: %s\nEspecialidade: %s\n\nAtendimento de segunda a sexta, %s.\n",
				o.city, o.addr[v], o.bairro[v], o.cep[v], o.phone[v], o.specialty, o.hours)
		}
		return b.String()
	}
	for v, d := range officeDates {
		g.doc("filiais", "file:///aurora/institucional/filiais.md", d, "Endereços das filiais", render(v))
	}

	asOfs := []string{"2026-03-15", "2026-07-15"}
	months := []string{"março", "julho"}
	for _, o := range g.offices {
		cur := o.addr[2]
		stale := distinct(o.addr, cur)
		var mnc []string
		for _, s := range stale {
			mnc = append(mnc, "Endereço: "+s)
		}
		tags := func(extra ...string) []string {
			if len(stale) > 0 {
				return append(extra, TagUpdate)
			}
			return extra
		}
		g.ask(Question{Question: fmt.Sprintf(pick(g.r, []string{"Qual é o endereço da filial de %s?", "Onde fica o escritório de %s?", "Em que rua está a filial de %s?"}), o.city),
			Gold: []Gold{{DocID: "filiais", Span: "Endereço: " + cur}}, Answers: []string{cur}, MustNotContain: mnc, Tags: tags()})
		g.ask(Question{Question: o.specialtyQ, Gold: []Gold{{DocID: "filiais", Span: "Endereço: " + cur}}, Answers: []string{cur},
			MustNotContain: mnc, Tags: tags(TagParaphrase)})
		for v := 0; v < 2; v++ {
			if o.addr[v] != o.addr[v+1] {
				old := o.addr[v]
				var other []string
				for _, s := range distinct(o.addr, old) {
					other = append(other, "Endereço: "+s)
				}
				g.ask(Question{Question: fmt.Sprintf("Onde ficava a filial de %s em %s de 2026?", o.city, months[v]), AsOf: asOfs[v],
					Gold: []Gold{{DocID: "filiais", Span: "Endereço: " + old}}, Answers: []string{old}, MustNotContain: other, Tags: []string{TagTemporal}})
			}
		}
		if o.phone[2] != o.phone[0] {
			g.ask(Question{Question: fmt.Sprintf("Qual o telefone da filial de %s?", o.city),
				Gold: []Gold{{DocID: "filiais", Span: "Telefone: " + o.phone[2]}}, Answers: []string{o.phone[2]},
				MustNotContain: []string{o.phone[0]}, Tags: []string{TagUpdate}})
		}
		g.train(Question{Question: fmt.Sprintf("Preciso mandar uma correspondência para a filial de %s, qual o CEP e o endereço?", o.city),
			Gold: []Gold{{DocID: "filiais", Span: "Endereço: " + cur}}, Answers: []string{cur}})
		g.gate(Question{Question: fmt.Sprintf("Em que bairro fica a filial de %s?", o.city),
			Gold: []Gold{{DocID: "filiais", Span: "Bairro: " + o.bairro[2]}}, Answers: []string{o.bairro[2]}})
	}
}

// ---------------------------------------------------------------------------
// People directory: one document per department, up to three versions, and
// session statements that change extensions later.

var departments = []struct {
	name, slug string
	roles      []string
	resp       [][2]string // responsibility in the document, paraphrased question
}{
	{"Financeiro", "financeiro",
		[]string{"Analista Financeiro", "Coordenadora Financeira", "Contador", "Assistente de Contas a Pagar", "Gerente Financeira", "Analista de Tesouraria", "Controller", "Especialista em Custos"},
		[][2]string{
			{"a conciliação bancária e o fechamento do caixa diário", "Quem confere se os extratos do banco batem com o sistema no fim do dia?"},
			{"o pagamento de fornecedores e a programação de boletos", "Com quem eu falo para saber quando a fatura de um parceiro vai ser quitada?"},
			{"a apuração de impostos e as obrigações fiscais mensais", "Quem calcula os tributos que a empresa recolhe todo mês?"},
			{"a cobrança de clientes inadimplentes", "Quem corre atrás de quem está devendo para a empresa?"},
			{"o orçamento anual e o acompanhamento das metas de despesa", "Quem planeja quanto cada área pode gastar no ano?"},
			{"o reembolso de despesas de viagem dos colaboradores", "Quem devolve o dinheiro que gastei com táxi e hotel numa viagem a trabalho?"},
		}},
	{"Comercial", "comercial",
		[]string{"Executivo de Contas", "Gerente Comercial", "Analista de Pré-Vendas", "Coordenador de Parcerias", "Representante de Vendas", "Diretora Comercial", "Especialista em Licitações", "Analista de Inteligência de Mercado"},
		[][2]string{
			{"as propostas para grandes contas e licitações públicas", "Quem prepara as ofertas para concorrências do governo?"},
			{"a renovação de contratos e a retenção da carteira", "Quem tenta convencer um cliente antigo a continuar com a gente?"},
			{"as demonstrações do produto para novos prospects", "Quem apresenta a plataforma funcionando para empresas interessadas?"},
			{"o programa de parceiros revendedores", "Quem cuida das empresas que vendem nossa solução para terceiros?"},
			{"a definição de descontos e da política comercial", "Quem autoriza um abatimento no valor para fechar negócio?"},
			{"o relacionamento com clientes do setor de saúde", "Quem atende hospitais e clínicas que compram de nós?"},
		}},
	{"Suporte", "suporte",
		[]string{"Analista de Suporte N1", "Analista de Suporte N2", "Líder de Suporte", "Especialista em Atendimento", "Coordenadora de Suporte", "Analista de Qualidade", "Gerente de Sucesso do Cliente", "Redator Técnico"},
		[][2]string{
			{"o atendimento por chat em primeiro nível", "Quem responde primeiro quando alguém abre uma conversa pedindo ajuda no site?"},
			{"a análise de falhas complexas encaminhadas pelo primeiro nível", "Para quem vão os problemas difíceis que o atendimento inicial não resolve?"},
			{"a base de conhecimento e os artigos de ajuda", "Quem escreve os tutoriais publicados para os usuários?"},
			{"a pesquisa de satisfação após cada atendimento", "Quem mede se os clientes ficaram contentes com o suporte?"},
			{"a escala de plantão de fim de semana", "Quem organiza quem trabalha no sábado e no domingo?"},
			{"o treinamento de novos atendentes", "Quem ensina o pessoal recém-contratado do help desk?"},
		}},
	{"Jurídico", "juridico",
		[]string{"Advogada Corporativa", "Paralegal", "Gerente Jurídico", "Analista de Contratos", "Advogado Trabalhista", "Coordenadora de Compliance", "Encarregado de Dados", "Diretora Jurídica"},
		[][2]string{
			{"a revisão de contratos com fornecedores", "Quem lê os acordos com prestadores antes da assinatura?"},
			{"os processos trabalhistas", "Quem acompanha as ações movidas por ex-funcionários?"},
			{"a proteção de dados pessoais e os pedidos de titulares", "A quem encaminho um cliente que quer saber que informações guardamos sobre ele?"},
			{"o registro de marcas e a propriedade intelectual", "Quem protege o nome e o logotipo da empresa no INPI?"},
			{"o programa de integridade e o canal de denúncias", "Quem recebe relatos anônimos de condutas irregulares?"},
			{"as procurações e a documentação societária", "Quem cuida das atas e alterações do contrato social?"},
		}},
	{"Recursos Humanos", "rh",
		[]string{"Analista de Recrutamento", "Business Partner de RH", "Coordenadora de Benefícios", "Analista de Departamento Pessoal", "Gerente de Pessoas", "Analista de Treinamento", "Especialista em Remuneração", "Diretora de Pessoas"},
		[][2]string{
			{"o recrutamento de vagas de tecnologia", "Quem contrata programadores para a empresa?"},
			{"a folha de pagamento e o cálculo de horas extras", "Quem calcula o salário e os adicionais no fim do mês?"},
			{"o plano de saúde e os benefícios", "Com quem falo para incluir meu filho no convênio médico?"},
			{"a avaliação de desempenho semestral", "Quem conduz o ciclo de feedback dos funcionários a cada seis meses?"},
			{"a integração de novos colaboradores", "Quem recebe o pessoal no primeiro dia de trabalho?"},
			{"os programas de estágio e jovem aprendiz", "Quem seleciona universitários para trabalhar meio período?"},
		}},
	{"Operações", "operacoes",
		[]string{"Analista de Logística", "Coordenador de Operações", "Supervisor de Facilities", "Comprador", "Gerente de Operações", "Analista de Processos", "Técnico de Manutenção", "Coordenadora de Segurança Patrimonial"},
		[][2]string{
			{"a frota de veículos e o controle de combustível", "Quem administra os carros da empresa e o abastecimento?"},
			{"as compras de equipamentos e mobiliário", "Quem adquire cadeiras, mesas e notebooks?"},
			{"a manutenção predial e a limpeza dos escritórios", "Quem eu chamo quando o ar-condicionado da sala quebra?"},
			{"o almoxarifado e o inventário de ativos", "Quem controla o estoque de materiais guardados?"},
			{"os contratos de logística e transporte de cargas", "Quem negocia com as transportadoras que levam nossas entregas?"},
			{"a segurança patrimonial e o controle de acesso", "Quem libera crachás e cuida das catracas da portaria?"},
		}},
	{"Tecnologia", "tecnologia",
		[]string{"Engenheira de Software", "Arquiteto de Soluções", "Analista de Infraestrutura", "Gerente de Tecnologia", "Especialista em Segurança", "Cientista de Dados", "Desenvolvedora Mobile", "Engenheiro de Dados"},
		[][2]string{
			{"a infraestrutura em nuvem e os servidores de produção", "Quem mantém no ar as máquinas onde o sistema roda?"},
			{"a segurança da informação e a resposta a incidentes", "Quem investiga quando há suspeita de invasão hacker?"},
			{"o aplicativo móvel da plataforma", "Quem desenvolve a versão do sistema para celular?"},
			{"os painéis de indicadores e a análise de dados", "Quem monta os relatórios com gráficos de desempenho do negócio?"},
			{"a integração com sistemas de clientes por API", "Quem conecta nosso software aos programas que os clientes já usam?"},
			{"a gestão de acessos e senhas dos sistemas internos", "Quem cria meu login quando entro na empresa?"},
		}},
}

var firstNames = []string{
	"Ana", "Bruno", "Carla", "Diego", "Elisa", "Fábio", "Gabriela", "Heitor", "Isabela", "João",
	"Karina", "Lucas", "Mariana", "Nicolas", "Olívia", "Paulo", "Quitéria", "Rafael", "Sabrina", "Tiago",
	"Úrsula", "Vinícius", "Wanda", "Xavier", "Yasmin", "Zeca", "Beatriz", "Caio", "Débora", "Eduardo",
	"Fernanda", "Gustavo", "Helena", "Igor", "Juliana", "Leonardo", "Marcela", "Otávio", "Patrícia", "Renato",
}

var lastNames = []string{
	"Ribeiro", "Almeida", "Carvalho", "Duarte", "Esteves", "Ferraz", "Gonçalves", "Holanda", "Igarashi", "Junqueira",
	"Kowalski", "Lacerda", "Macedo", "Nogueira", "Oliveira", "Prado", "Queiroz", "Rezende", "Siqueira", "Teixeira",
	"Uchoa", "Valadares", "Wagner", "Xavante", "Yamamoto", "Zanetti", "Barros", "Cordeiro", "Dantas", "Espíndola",
	"Fontes", "Guimarães", "Hoffmann", "Lima", "Monteiro", "Pacheco", "Rocha", "Sampaio", "Tavares", "Vasconcelos",
}

var dirDates = []string{"2026-01-05", "2026-06-01", "2026-10-05"}

type person struct {
	name, dept, slug, email, city string
	roles                         []string // per directory version (3)
	ramals                        []int    // per directory version (3)
	sessRamal                     int      // extension stated later in a session (0 = none)
	sessID, sessDate              string
	resp, respQ                   string
	verb                          string
}

func (p *person) curRamal() int {
	if p.sessRamal != 0 {
		return p.sessRamal
	}
	return p.ramals[2]
}

func (p *person) respSpan() string { return p.name + " " + p.verb + " " + p.resp }

func (g *synth) newName() string {
	for {
		n := pick(g.r, firstNames) + " " + pick(g.r, lastNames)
		if !g.names[n] {
			g.names[n] = true
			return n
		}
	}
}

func (g *synth) genDirectory() {
	byDept := map[string][]*person{}
	spare := map[string][]string{}
	verbs := []string{"responde por", "é a referência para", "cuida de"}
	for _, d := range departments {
		roles := shuffled(g.r, d.roles)
		resp := shuffled(g.r, d.resp)
		for i := 0; i < 6; i++ {
			name := g.newName()
			slug := strings.ReplaceAll(Normalize(name), " ", ".")
			r := g.ramal()
			p := &person{name: name, dept: d.name, slug: d.slug, email: slug + "@" + domain, city: pick(g.r, g.offices).city,
				roles: []string{roles[i], roles[i], roles[i]}, ramals: []int{r, r, r}, resp: resp[i][0], respQ: resp[i][1],
				verb: pick(g.r, verbs)}
			g.people = append(g.people, p)
			byDept[d.slug] = append(byDept[d.slug], p)
		}
		spare[d.slug] = roles[6:]
	}
	// Five departments change in v2, three of them again in v3.
	order := shuffled(g.r, []int{0, 1, 2, 3, 4, 5, 6})
	changed := map[string]int{} // slug -> last changed version
	for k, di := range order[:5] {
		slug := departments[di].slug
		ps := shuffled(g.r, byDept[slug])
		promoted, moved := ps[0], ps[1]
		promoted.roles[1], promoted.roles[2] = spare[slug][0], spare[slug][0]
		nr := g.ramal()
		moved.ramals[1], moved.ramals[2] = nr, nr
		changed[slug] = 1
		if k < 3 {
			changed[slug] = 2
			if k < 2 {
				promoted.roles[2] = spare[slug][1] // a three-value job title chain
			} else {
				ps[2].ramals[2] = g.ramal()
			}
			if k == 0 {
				moved.ramals[2] = g.ramal() // a three-value extension chain in documents
			}
		}
	}
	// Session statements change extensions later: two people whose extension
	// already changed in a document (a chain doc v1 -> doc v2 -> session) and
	// two people with no other change.
	sessDates := []string{"2026-09-08", "2026-09-22", "2026-10-13", "2026-10-20"}
	var chain, stable []*person
	for _, p := range g.people {
		switch {
		case p.ramals[1] != p.ramals[0] && p.ramals[2] == p.ramals[1]:
			chain = append(chain, p)
		case p.ramals[2] == p.ramals[0] && p.roles[2] == p.roles[0]:
			stable = append(stable, p)
		}
	}
	upd := append(shuffled(g.r, chain)[:min(2, len(chain))], shuffled(g.r, stable)[:2]...)
	for i, p := range upd {
		p.sessRamal, p.sessID, p.sessDate = g.ramal(), fmt.Sprintf("sess-ramal-%d", i+1), sessDates[i]
	}

	render := func(d string, slug string, v int) string {
		var b strings.Builder
		fmt.Fprintf(&b, "# Equipe de %s\n\nDiretório interno da equipe de %s da %s. Atualizado em %s.\n", d, d, company, brDate(dirDates[v]))
		for _, p := range byDept[slug] {
			fmt.Fprintf(&b, "\n## %s\n\nCargo: %s\nRamal: %d\nE-mail: %s\nEscritório: %s\n\n%s.\n",
				p.name, p.roles[v], p.ramals[v], p.email, p.city, p.respSpan())
		}
		fmt.Fprintf(&b, "\n## Contato geral\n\nO e-mail geral da equipe de %s é %s@%s.\n", d, slug, domain)
		return b.String()
	}
	for v, date := range dirDates {
		for _, d := range departments {
			if v == 0 || changed[d.slug] >= v {
				g.doc("dir-"+d.slug, "file:///aurora/diretorio/"+d.slug+".md", date, "Equipe de "+d.name, render(d.name, d.slug, v))
			}
		}
	}
	for _, p := range upd {
		g.session(p.sessID, p.sessDate, []Statement{{Subject: p.name, Predicate: "ramal", Value: strconv.Itoa(p.sessRamal)}},
			userTurn(fmt.Sprintf("Atualização de cadastro: a partir de hoje o ramal de %s passa a ser %d.", p.name, p.sessRamal)),
			botTurn("Entendido, registrei o novo ramal."))
	}

	// Questions.
	cargoAsOf := []struct{ asOf, month string }{{"2026-03-15", "março"}, {"2026-08-15", "agosto"}}
	ramalAsOf := []struct{ asOf, month string }{{"2026-04-15", "abril"}, {"2026-08-20", "agosto"}}
	for _, d := range departments {
		ps := byDept[d.slug]
		docID := "dir-" + d.slug
		g.ask(Question{Question: fmt.Sprintf("Qual é o e-mail geral da equipe de %s?", d.name),
			Gold: []Gold{{DocID: docID, Span: "e-mail geral da equipe de " + d.name}}, Answers: []string{d.slug + "@" + domain}})
		for i, p := range ps {
			cur := p.roles[2]
			staleRoles := distinct(p.roles, cur)
			q := Question{
				Question: fmt.Sprintf(pick(g.r, []string{"Qual é o cargo de %s?", "Que função %s exerce na empresa?", "%s trabalha em qual cargo?"}), p.name),
				Gold:     []Gold{{DocID: docID, Span: "Cargo: " + cur}}, Answers: []string{cur},
			}
			for _, r := range staleRoles {
				q.MustNotContain = append(q.MustNotContain, "Cargo: "+r)
			}
			if len(staleRoles) > 0 {
				q.Tags = []string{TagUpdate}
				g.ask(Question{Question: fmt.Sprintf("Quem ocupa hoje o cargo de %s na equipe de %s?", cur, d.name),
					Gold: []Gold{{DocID: docID, Span: "Cargo: " + cur}}, Answers: []string{p.name}, Tags: []string{TagUpdate}})
				for v := 0; v < 2; v++ {
					if p.roles[v] != p.roles[v+1] {
						old := p.roles[v]
						var other []string
						for _, r := range distinct(p.roles, old) {
							other = append(other, "Cargo: "+r)
						}
						g.ask(Question{Question: fmt.Sprintf("Qual era o cargo de %s em %s de 2026?", p.name, cargoAsOf[v].month), AsOf: cargoAsOf[v].asOf,
							Gold: []Gold{{DocID: docID, Span: "Cargo: " + old}}, Answers: []string{old}, MustNotContain: other, Tags: []string{TagTemporal}})
					}
				}
			}
			g.ask(q)
			g.ask(Question{Question: p.respQ, Gold: []Gold{{DocID: docID, Span: p.respSpan()}}, Answers: []string{p.name}, Tags: []string{TagParaphrase}})

			// Extension: every person whose extension ever changed, and half of the others.
			vals := []int{p.ramals[0], p.ramals[1], p.ramals[2]}
			if p.sessRamal != 0 {
				vals = append(vals, p.sessRamal)
			}
			curR := p.curRamal()
			staleR := distinct(vals, curR)
			if len(staleR) > 0 || i%2 == 0 {
				q := Question{Question: fmt.Sprintf(pick(g.r, []string{"Qual é o ramal de %s?", "Em que ramal falo com %s?"}), p.name), Answers: []string{strconv.Itoa(curR)}}
				if p.sessRamal != 0 {
					q.Gold = []Gold{{DocID: p.sessID, Span: fmt.Sprintf("ramal: %d", curR)}}
					q.Tags = []string{TagSession, TagUpdate}
				} else {
					q.Gold = []Gold{{DocID: docID, Span: fmt.Sprintf("Ramal: %d", curR)}}
					if len(staleR) > 0 {
						q.Tags = []string{TagUpdate}
					}
				}
				for _, r := range staleR {
					q.MustNotContain = append(q.MustNotContain, fmt.Sprintf("ramal: %d", r))
				}
				g.ask(q)
			}
			for v := 0; v < 2; v++ {
				// the value that held in the period before each document change
				before, after := p.ramals[v], p.ramals[v+1]
				if before == after {
					continue
				}
				var other []string
				for _, r := range distinct(vals, before) {
					other = append(other, fmt.Sprintf("ramal: %d", r))
				}
				g.ask(Question{Question: fmt.Sprintf("Qual era o ramal de %s em %s de 2026?", p.name, ramalAsOf[v].month), AsOf: ramalAsOf[v].asOf,
					Gold: []Gold{{DocID: docID, Span: fmt.Sprintf("Ramal: %d", before)}}, Answers: []string{strconv.Itoa(before)},
					MustNotContain: other, Tags: []string{TagTemporal}})
			}
			if p.sessRamal != 0 && p.ramals[2] != p.ramals[0] {
				// chain doc v1 -> doc v2 -> session: the document value held until the statement
				var other []string
				for _, r := range distinct(vals, p.ramals[2]) {
					other = append(other, fmt.Sprintf("ramal: %d", r))
				}
				g.ask(Question{Question: fmt.Sprintf("Qual era o ramal de %s no fim de agosto de 2026?", p.name), AsOf: "2026-08-31",
					Gold: []Gold{{DocID: docID, Span: fmt.Sprintf("Ramal: %d", p.ramals[2])}}, Answers: []string{strconv.Itoa(p.ramals[2])},
					MustNotContain: other, Tags: []string{TagTemporal}})
			}
			if i%3 == 1 {
				g.ask(Question{Question: fmt.Sprintf(pick(g.r, []string{"Qual é o e-mail de %s?", "Como mando um e-mail para %s?"}), p.name),
					Gold: []Gold{{DocID: docID, Span: "E-mail: " + p.email}}, Answers: []string{p.email}})
			}
			// Training: other wordings of the same tasks.
			g.train(Question{Question: fmt.Sprintf("Me passa o contato de %s, preciso falar sobre %s.", p.name, p.resp),
				Gold: []Gold{{DocID: docID, Span: p.respSpan()}}, Answers: []string{p.name}})
			if i%3 == 2 {
				g.gate(Question{Question: fmt.Sprintf("Em qual escritório trabalha %s?", p.name),
					Gold: []Gold{{DocID: docID, Span: "Escritório: " + p.city}}, Answers: []string{p.city}})
			}
			if i%2 == 1 {
				g.train(Question{Question: fmt.Sprintf("Em que escritório e com qual cargo trabalha %s?", p.name),
					Gold: []Gold{{DocID: docID, Span: "Cargo: " + cur}}, Answers: []string{cur}})
			}
		}
	}
}
