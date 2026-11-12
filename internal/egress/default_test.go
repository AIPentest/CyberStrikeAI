package egress

import "testing"

func TestDefaultBeforeSetIsDisabledPassthrough(t *testing.T) {
	// Reset in case other tests set it.
	SetDefault(nil)
	g := Default()
	if g == nil {
		t.Fatal("Default() returned nil")
	}
	if g.Enabled() {
		t.Fatal("default before SetDefault should be a disabled passthrough")
	}
	if tr := g.Transport(); tr == nil || tr.DialContext == nil {
		t.Fatal("passthrough transport unusable")
	}
}

func TestSetDefaultInstallsGateway(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeTor})
	SetDefault(g)
	defer SetDefault(nil)
	if !Default().Enabled() {
		t.Fatal("installed gateway should be enabled via Default()")
	}
}
