package scheduler

import "testing"

func TestRequiredWorkers(t *testing.T) {
	tests := []struct {
		rps, capacity, want int
	}{
		{50, 100, 1},
		{100, 100, 1},
		{101, 100, 2},
		{350, 100, 4},
	}
	for _, tt := range tests {
		got, err := RequiredWorkers(tt.rps, tt.capacity)
		if err != nil || got != tt.want {
			t.Fatalf("RequiredWorkers(%d, %d) = %d, %v; want %d", tt.rps, tt.capacity, got, err, tt.want)
		}
	}
}

func TestDistributeRPS(t *testing.T) {
	tests := []struct {
		total, workers int
		want           []int
	}{
		{400, 4, []int{100, 100, 100, 100}},
		{350, 4, []int{88, 88, 87, 87}},
	}
	for _, tt := range tests {
		got, err := DistributeRPS(tt.total, tt.workers)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(tt.want) {
			t.Fatalf("got %v, want %v", got, tt.want)
		}
		sum := 0
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			sum += got[i]
		}
		if sum != tt.total {
			t.Fatalf("allocation sum = %d, want %d", sum, tt.total)
		}
	}
}
