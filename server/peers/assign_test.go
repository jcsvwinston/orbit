package peers

import "testing"

func TestOwner_DeterministicAndSpread(t *testing.T) {
	eps := []string{"http://b:9090", "http://a:9090", "http://c:9090"}
	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		id := "node-" + string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('A'+i%7))
		o1 := Owner(id, eps)
		o2 := Owner(id, []string{eps[2], eps[0], eps[1]}) // order does not matter
		if o1 != o2 {
			t.Fatalf("owner of %q depends on endpoint order: %s vs %s", id, o1, o2)
		}
		counts[o1]++
	}
	for _, ep := range eps {
		if counts[ep] < 50 {
			t.Fatalf("assignment does not spread: %v", counts)
		}
	}
	if Owner("x", nil) != "" || Owner("x", []string{" ", ""}) != "" {
		t.Fatal("no endpoints own nothing")
	}
	// Removing a server moves only its nodes.
	moved, kept := 0, 0
	for i := 0; i < 300; i++ {
		id := "n" + string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('A'+i%7))
		before := Owner(id, eps)
		after := Owner(id, []string{"http://a:9090", "http://b:9090"})
		switch {
		case before == "http://c:9090":
			moved++
		case before != after:
			t.Fatalf("node %q owned by %s moved to %s although its owner stayed", id, before, after)
		default:
			kept++
		}
	}
	if moved == 0 || kept == 0 {
		t.Fatalf("moved=%d kept=%d", moved, kept)
	}
}
