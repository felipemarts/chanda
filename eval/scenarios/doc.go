// Package scenarios is phase 5 of the evaluation battery: scripted,
// deterministic scenarios that drive a real `chanda serve` process over MCP
// stdio (and the chanda CLI where a person would act), without any LLM.
//
// Each scenario has an id, an area (forget, document update, injection,
// temporal, dedup, abstention), the steps it runs, and a list of checks
// with the expected and the measured value. A scenario passes only when
// every check passes. The checks are independent of Chanda's own
// self-reports: forgetting is verified by searching through MCP and by
// scanning the raw bytes of the database, WAL and SHM files and every row
// of a copy of the database, not by trusting forget's verification probes.
//
// The whole set runs twice on fresh databases; the two result documents
// must be byte-identical. Results are committed in results.json and
// results.md:
//
//	go test ./eval/scenarios/ -run TestScenarios -update   # regenerate
//	go test ./eval/scenarios/                              # check only
//
// The check-only run fails when any scenario fails, when the two runs
// differ, or when the committed files differ from what the run produced.
package scenarios
