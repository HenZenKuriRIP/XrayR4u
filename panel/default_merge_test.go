package panel

import (
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/service/controller"
	"github.com/imdario/mergo"
)

func TestOmittedSendIPKeepsDefault(t *testing.T) {
	def := getDefaultControllerConfig()
	src := &controller.Config{ListenIP: "10.1.2.3", UpdatePeriodic: 30}
	if err := mergo.Merge(def, src, mergo.WithOverride); err != nil {
		t.Fatal(err)
	}
	if def.ListenIP != "10.1.2.3" || def.SendIP != "0.0.0.0" {
		t.Fatalf("listen=%s send=%s", def.ListenIP, def.SendIP)
	}
}

func TestExplicitSendIPOverridesDefault(t *testing.T) {
	def := getDefaultControllerConfig()
	src := &controller.Config{SendIP: "203.0.113.10", ListenIP: "0.0.0.0"}
	if err := mergo.Merge(def, src, mergo.WithOverride); err != nil {
		t.Fatal(err)
	}
	if def.SendIP != "203.0.113.10" {
		t.Fatalf("send=%s", def.SendIP)
	}
}

func TestEmptySendIPDoesNotClearDefault(t *testing.T) {
	def := getDefaultControllerConfig()
	src := &controller.Config{SendIP: ""}
	if err := mergo.Merge(def, src, mergo.WithOverride); err != nil {
		t.Fatal(err)
	}
	if def.SendIP != "0.0.0.0" || def.ListenIP != "0.0.0.0" {
		t.Fatalf("listen=%s send=%s", def.ListenIP, def.SendIP)
	}
}
