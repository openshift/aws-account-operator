package account

import (
	"reflect"
	"testing"
)

func TestCreateSecret(t *testing.T) {
	testData := map[string][]byte{
		"one": []byte("hello"),
		"two": []byte("world"),
	}
	tables := []struct {
		name      string
		namespace string
		data      map[string][]byte
	}{
		{"test", "namespace", testData},
	}

	for _, table := range tables {
		secret := CreateSecret(table.name, table.namespace, table.data)
		if secret.ObjectMeta.Name != table.name {
			t.Errorf("Secret name does not match.  Got %s want %s", secret.ObjectMeta.Name, table.name)
		}
		if secret.ObjectMeta.Namespace != table.namespace {
			t.Errorf("Secret namespace does not match.  Got %s want %s", secret.ObjectMeta.Namespace, table.namespace)
		}
		if !reflect.DeepEqual(secret.Data, table.data) {
			t.Errorf("Secret Data map is not equal.  Got %s want %s", secret.Data, table.data)
		}
	}
}
