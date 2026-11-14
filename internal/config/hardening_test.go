package config

import "testing"

// Transport and memory settings do not decide behaviour: they stay out of
// the configuration hash (decisions 0100, 0101), like mcp.http_addr.
func TestHashIgnoresTransportAndMemoryTuning(t *testing.T) {
	c := Default()
	if c.MCP.HTTPTokenEnv != "CHANDA_MCP_TOKEN" || c.Store.CacheMB != 256 || c.Sleep.StaleRunMinutes != 60 {
		t.Fatalf("defaults: %+v %+v %d", c.MCP, c.Store, c.Sleep.StaleRunMinutes)
	}
	h := c.Hash()
	c.MCP.HTTPTokenEnv, c.Store = "OTHER", StoreConfig{CacheMB: 1, MmapMB: 64}
	if c.Hash() != h {
		t.Fatal("hash changed with mcp.http_token_env or store")
	}
	c.Sleep.StaleRunMinutes = 5
	if c.Hash() == h {
		t.Fatal("hash ignores sleep.stale_run_minutes")
	}
}
