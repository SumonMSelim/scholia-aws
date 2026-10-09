package awscfg

import "testing"

func TestLoadRequiresRegion(t *testing.T) {
	if _, err := Load(t.Context(), "", ""); err == nil {
		t.Fatal("empty region was accepted")
	}
}

func TestLoadAcceptsRegionAndEndpoint(t *testing.T) {
	cfg, err := Load(t.Context(), "us-east-1", "http://127.0.0.1:4566")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("region %s", cfg.Region)
	}
	prod, err := Load(t.Context(), "us-east-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if prod.Region != "us-east-1" {
		t.Fatalf("region %s", prod.Region)
	}
}
