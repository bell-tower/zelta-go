package cmdbuild

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildSEND(t *testing.T) {
	argv, err := Build("SEND", map[string]string{
		"flags":     "",
		"intr_snap": "-I tank/src@a",
		"ds_snap":   "tank/src@b",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs send -P -I tank/src@a tank/src@b"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildRECV(t *testing.T) {
	argv, err := Build("RECV", map[string]string{
		"flags":  "-o readonly=on",
		"origin": "",
		"ds":     "tank/tgt",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs recv -v -o readonly=on tank/tgt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildFullSend(t *testing.T) {
	argv, err := Build("SEND", map[string]string{
		"ds_snap": "tank/src@a",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs send -P tank/src@a"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRemoteRoles(t *testing.T) {
	for _, tc := range []struct {
		action, role string
		stdinNull    bool
	}{
		{"SEND", RoleSend, true},
		{"SEND_CHECK", RoleSend, true},
		{"RECV", RoleRecv, false},
		{"LIST", RoleDefault, true},
		{"SNAP", RoleDefault, true},
		{"MATCH", RoleNone, true},
	} {
		role, err := RemoteRole(tc.action)
		if err != nil {
			t.Fatal(err)
		}
		if role != tc.role {
			t.Fatalf("%s role=%q want %q", tc.action, role, tc.role)
		}
		if StdinNull(role) != tc.stdinNull {
			t.Fatalf("%s StdinNull=%v want %v", tc.action, StdinNull(role), tc.stdinNull)
		}
	}
}

func TestListArgv(t *testing.T) {
	argv, err := ListArgv([]string{"name", "guid"}, 2, "tank/src")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs list -Hpr -o name,guid -t all -S createtxg -d 2 tank/src"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSnapArgv(t *testing.T) {
	argv, err := SnapArgv("tank/src@a")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs snapshot -r tank/src@a"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCreateArgv(t *testing.T) {
	argv, err := CreateArgv("tank/parent")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs create -vupo canmount=noauto tank/parent"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCheckArgv(t *testing.T) {
	argv, err := CheckArgv("tank/parent")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	want := "zfs list -Ho name tank/parent"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBookmarkArgv(t *testing.T) {
	got, err := BookmarkArgv("tank/src@daily", "tank/src#backup_daily")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zfs", "bookmark", "tank/src@daily", "tank/src#backup_daily"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCloneAndRenameArgv(t *testing.T) {
	clone, err := CloneArgv("tank/src@daily", "tank/clone")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"zfs", "clone", "-p", "-o", "readonly=off", "tank/src@daily", "tank/clone"}; !reflect.DeepEqual(clone, want) {
		t.Fatalf("clone=%v want %v", clone, want)
	}
	rename, err := RenameArgv("tank/live", "tank/live_daily")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"zfs", "rename", "-fp", "tank/live", "tank/live_daily"}; !reflect.DeepEqual(rename, want) {
		t.Fatalf("rename=%v want %v", rename, want)
	}
}
