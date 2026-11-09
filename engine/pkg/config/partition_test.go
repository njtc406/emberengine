package config

import (
	"github.com/njtc406/viper"
	"strings"
	"testing"
)

func TestPartitionConfigString(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{{`"001"`, "001"}, {`"1"`, "1"}, {`"0"`, "0"}, {`"zone.eu"`, "zone.eu"}, {`1`, "1"}} {
		t.Run(tc.yaml, func(t *testing.T) {
			v := viper.New()
			v.SetConfigType("yaml")
			if err := v.ReadConfig(strings.NewReader("Partition: " + tc.yaml)); err != nil {
				t.Fatal(err)
			}
			var conf ServiceInitConf
			if err := v.Unmarshal(&conf); err != nil {
				t.Fatal(err)
			}
			if conf.Partition != tc.want {
				t.Fatalf("partition = %q, want %q", conf.Partition, tc.want)
			}
		})
	}
}
