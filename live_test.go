package nacostoolkit

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveNacos(t *testing.T) {
	if os.Getenv("NACOS_LIVE_TEST") != "1" {
		t.Skip("requires authorized Nacos test settings")
	}
	conn := Connection{
		ServerAddr: os.Getenv("NACOS_ADDR"),
		Namespace:  os.Getenv("NACOS_NS"),
		Username:   os.Getenv("NACOS_USER"),
		Password:   os.Getenv("NACOS_PASS"),
	}
	ref := ConfigRef{DataID: os.Getenv("NACOS_DATA_ID"), Group: os.Getenv("NACOS_GROUP")}
	if conn.ServerAddr == "" || ref.DataID == "" || ref.Group == "" {
		t.Fatal("NACOS_ADDR, NACOS_DATA_ID and NACOS_GROUP are required")
	}
	source, err := NewHTTPSource(conn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	text, err := source.GetConfig(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decode([]byte(text), YAML)
	if err != nil {
		t.Fatal("Nacos response could not be parsed as YAML")
	}
	if _, ok := value.(map[string]any); !ok {
		t.Fatal("Nacos response was not a configuration object")
	}
}
