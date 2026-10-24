//go:build linux

package main

import (
	"context"
	"fmt"
	"testing"
)

func TestApplyLinuxSystemProxyCommitsModeLast(t *testing.T) {
	var writes []string
	err := applyLinuxSystemProxy(context.Background(), "127.0.0.1", 48123, func(_ context.Context, schema, key, value string) error {
		writes = append(writes, fmt.Sprintf("%s:%s=%s", schema, key, value))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != len(linuxManagedProxyKeys) || writes[len(writes)-1] != "org.gnome.system.proxy:mode='manual'" {
		t.Fatalf("writes=%v", writes)
	}
}

func TestRestoreLinuxSystemProxyUsesExactSnapshotAndModeLast(t *testing.T) {
	snapshot := SystemProxySnapshot{Platform: "linux", Values: make(map[string]string)}
	for index, item := range linuxManagedProxyKeys {
		snapshot.Values[linuxProxyKeyID(item.schema, item.key)] = fmt.Sprintf("value-%d", index)
	}
	var writes []string
	err := restoreLinuxSystemProxy(context.Background(), snapshot, func(_ context.Context, schema, key, value string) error {
		writes = append(writes, linuxProxyKeyID(schema, key)+"="+value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	last := linuxManagedProxyKeys[len(linuxManagedProxyKeys)-1]
	want := linuxProxyKeyID(last.schema, last.key) + "=value-5"
	if len(writes) != len(linuxManagedProxyKeys) || writes[len(writes)-1] != want {
		t.Fatalf("writes=%v want last=%q", writes, want)
	}
}

func TestRestoreLinuxSystemProxyRejectsIncompleteSnapshot(t *testing.T) {
	err := restoreLinuxSystemProxy(context.Background(), SystemProxySnapshot{Platform: "linux", Values: map[string]string{}}, func(context.Context, string, string, string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected incomplete snapshot error")
	}
}
