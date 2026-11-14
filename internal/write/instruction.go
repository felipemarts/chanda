package write

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Instruction-like detector (BASE.md write step 3): text from the external
// zone that is shaped like an instruction to the assistant gets the
// core.FlagInstructionLike mark. The mark never removes content; it keeps the
// text out of the context pre-context and labels it in search.
//
// The detector is rule-based and deterministic. Each built-in rule is a
// regular expression matched against the folded text: lowercase, diacritics
// removed (so "instruções" and "instrucoes" match the same rule), typographic
// apostrophes replaced by "'". Rules cover English and Portuguese:
//
//	override_previous   "ignore previous instructions", "ignore as instruções"
//	system_prompt       any mention of the "system prompt" / "prompt do sistema"
//	reveal_prompt       "reveal your instructions", "mostre o seu prompt"
//	obligation          "you must", "você deve", "você precisa"
//	role_reassign       "you are now", "from now on you", "pretend to be", "a partir de agora você"
//	addressed_directive "Assistant: send ...", "note to the AI", "se você é uma IA"
//	new_instructions    "new instructions:", "novas instruções:"
//	role_tag            chat-template markers: "<|system|>", "[INST]", "<system>"
//	conceal_from_user   "do not tell the user", "não conte ao usuário"
//	memory_tool_call    "call assert_fact", "use the forget tool"
//
// Extra patterns from write.instruction_patterns (config) are matched against
// the normalized, unfolded text and reported as rule "custom".
type instructionRule struct {
	name string
	re   *regexp.Regexp
}

var builtinInstructionRules = func() []instructionRule {
	src := []struct{ name, re string }{
		// English.
		{"override_previous", `\b(ignore|disregard|forget|override|bypass)\s+(all\s+|any\s+)?(of\s+)?(the\s+|your\s+|my\s+|these\s+|those\s+)?(previous|prior|above|earlier|preceding|original|system|existing|initial)\s+(instructions?|prompts?|rules|guidelines|directives|directions|messages|context)\b`},
		{"override_previous", `\b(ignore|disregard|override|bypass)\s+(all\s+|any\s+)?(of\s+)?(the\s+|your\s+|my\s+|these\s+|those\s+)?(instructions|system\s+prompt)\b`},
		{"system_prompt", `\bsystem\s+prompts?\b`},
		{"reveal_prompt", `\b(reveal|print|show|repeat|output|leak|dump)\s+(me\s+)?(your|the)\s+(hidden\s+|initial\s+|original\s+|secret\s+)?(prompt|instructions)\b`},
		{"obligation", `\byou\s+(must|are\s+(now\s+)?required\s+to|have\s+to\s+now)\b`},
		{"role_reassign", `\b(you\s+are\s+now|from\s+now\s+on,?\s+(you|always|never)|pretend\s+(to\s+be|you\s+are|that\s+you)|roleplay\s+as|act\s+as\s+(if\s+you|an?\s+(ai|assistant|admin|administrator|developer|system|unrestricted|jailbroken)))\b`},
		{"addressed_directive", `\b(assistant|ai|chatbot|llm|agent|model|claude|chatgpt|gpt)\s*[,:]\s*(please\s+)?(ignore|do|don't|send|run|execute|delete|remove|forward|reply|respond|say|tell|write|call|open|visit|reveal|output|print|remember|store|save|add|email|transfer|update|change)\b`},
		{"addressed_directive", `\b(note|message|instructions?|attention)\s+(to|for)\s+(the\s+|any\s+|all\s+)?(ai|assistant|agent|llm|model|language\s+model|bot)s?\b`},
		{"addressed_directive", `\bif\s+you\s+are\s+an?\s+(ai|assistant|agent|llm|language\s+model|bot)\b`},
		{"new_instructions", `\b(new|updated|additional|real|actual|important)\s+(instructions?|directives?|orders)\s*:`},
		{"role_tag", `<\|\s*(system|im_start|im_end|endoftext)\s*\|>|</?(system|instructions?)>|\[/?(inst|system)\]`},
		{"conceal_from_user", `\b(do\s+not|don't|never)\s+(tell|inform|notify|alert|mention\s+(this\s+)?to|reveal\s+(this\s+)?to)\s+the\s+user\b`},
		{"conceal_from_user", `\bwithout\s+(telling|informing|notifying|asking)\s+the\s+user\b`},
		{"memory_tool_call", `\b(call|invoke|use|run)\s+(the\s+)?((remember|forget|promote|ingest)\s+(tool|function)|assert_fact|log_session|sleep_run|ranking_reset)\b`},

		// Portuguese (folded: no accents).
		{"override_previous", `\b(ignore|ignora|ignorem|ignorar|desconsidere|desconsiderar|desconsiderem|esqueca|esquecam|esquecer|descarte)\s+(todas\s+|todos\s+)?(as\s+|os\s+|suas\s+|seus\s+|essas\s+|estas\s+)?(instrucoes|orientacoes|diretrizes)\b`},
		{"override_previous", `\b(ignore|ignora|ignorem|ignorar|desconsidere|desconsiderar|desconsiderem|esqueca|esquecam|esquecer|descarte)\s+(todas\s+|todos\s+)?(as\s+|os\s+|suas\s+|seus\s+|essas\s+|estas\s+)?(ordens|regras|mensagens|comandos)\s+(anteriores|acima|previas|originais|iniciais|do\s+sistema)\b`},
		{"system_prompt", `\b(prompt|instrucoes|mensagem)\s+(do|de)\s+sistema\b`},
		{"reveal_prompt", `\b(revele|mostre|repita|imprima|exiba)\s+(o\s+seu\s+|o\s+|seu\s+|suas\s+|as\s+)(prompt|instrucoes)\b`},
		{"obligation", `\bvoce\s+(deve|precisa|tem\s+que|tem\s+de|e\s+obrigado\s+a|esta\s+obrigado\s+a)\b`},
		{"role_reassign", `\b(a\s+partir\s+de\s+agora,?\s+(voce|sempre|nunca)|voce\s+agora\s+e|finja\s+(ser|que)|aja\s+como\s+(uma?\s+)?(ia|assistente|administrador|desenvolvedor|sistema)|atue\s+como\s+(uma?\s+)?(ia|assistente|administrador|desenvolvedor|sistema))\b`},
		{"addressed_directive", `\b(assistente|ia|agente|modelo|robo|bot)\s*[,:]\s*(por\s+favor,?\s+)?(ignore|faca|envie|execute|rode|apague|delete|remova|encaminhe|responda|diga|escreva|chame|abra|acesse|revele|lembre|memorize|salve|guarde|adicione|transfira|altere|mude)\b`},
		{"addressed_directive", `\b(nota|mensagem|instrucoes|instrucao|atencao)\s+(para|ao|a)\s+(o\s+|a\s+)?(ia|assistente|agente|modelo)\b`},
		{"addressed_directive", `\bse\s+voce\s+(e|for)\s+(uma?\s+)?(ia|assistente|agente|modelo\s+de\s+linguagem)\b`},
		{"new_instructions", `\b(novas\s+(instrucoes|ordens|diretrizes)|nova\s+instrucao|instrucoes\s+(importantes|reais|verdadeiras|atualizadas))\s*:`},
		{"conceal_from_user", `\bnao\s+(conte|diga|informe|mencione|revele|avise)\s+(isso\s+|nada\s+)?(ao|para\s+o|a)\s+usuario\b`},
		{"conceal_from_user", `\bsem\s+(contar|avisar|informar|perguntar)\s+(ao|o|para\s+o)\s+usuario\b`},
		{"memory_tool_call", `\b(chame|use|execute|invoque|rode)\s+(a\s+|o\s+)?((ferramenta\s+)(remember|forget|promote|ingest)|assert_fact|log_session|sleep_run|ranking_reset)\b`},
	}
	out := make([]instructionRule, len(src))
	for i, r := range src {
		out[i] = instructionRule{r.name, regexp.MustCompile(r.re)}
	}
	return out
}()

// fold lowercases s, removes diacritics and unifies apostrophes.
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	f, _, err := transform.String(t, s)
	if err != nil {
		f = s
	}
	f = strings.NewReplacer("’", "'", "‘", "'", "ʼ", "'").Replace(f)
	return strings.ToLower(f)
}

// instructionRules returns the sorted, de-duplicated names of the rules that
// match text (already normalized). custom patterns report as "custom".
func instructionRules(text string, custom []*regexp.Regexp) []string {
	f := fold(text)
	seen := map[string]bool{}
	for _, r := range builtinInstructionRules {
		if !seen[r.name] && r.re.MatchString(f) {
			seen[r.name] = true
		}
	}
	for _, re := range custom {
		if re.MatchString(text) {
			seen["custom"] = true
			break
		}
	}
	return sortedKeys(seen)
}
