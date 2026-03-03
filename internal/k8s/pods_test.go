package k8s

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makePod(name string, ts time.Time, labelKey, labelVal string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Time{Time: ts},
			Labels:            map[string]string{labelKey: labelVal},
		},
	}
}

func TestSelectMostRecentPod(t *testing.T) {
	now := time.Now()
	older := now.Add(-5 * time.Minute)
	oldest := now.Add(-10 * time.Minute)

	pods := []corev1.Pod{
		makePod("oldest", oldest, "app", "myapp"),
		makePod("newest", now, "app", "myapp"),
		makePod("middle", older, "app", "myapp"),
	}

	got := SelectMostRecentPod(pods)
	if got == nil {
		t.Fatal("expected a pod, got nil")
	}
	if got.Name != "newest" {
		t.Errorf("expected newest, got %s", got.Name)
	}
}

func TestSelectMostRecentPod_Empty(t *testing.T) {
	if got := SelectMostRecentPod(nil); got != nil {
		t.Errorf("expected nil for empty slice, got %s", got.Name)
	}
}

func TestSelectMostRecentPod_SinglePod(t *testing.T) {
	pods := []corev1.Pod{makePod("only", time.Now(), "app", "myapp")}
	got := SelectMostRecentPod(pods)
	if got == nil || got.Name != "only" {
		t.Errorf("expected 'only', got %v", got)
	}
}

func TestPodNames(t *testing.T) {
	now := time.Now()
	pods := []corev1.Pod{
		makePod("a", now, "app", "x"),
		makePod("b", now, "app", "x"),
		makePod("c", now, "app", "x"),
	}

	t.Run("limit greater than count", func(t *testing.T) {
		names := PodNames(pods, 10)
		if len(names) != 3 {
			t.Errorf("expected 3, got %d", len(names))
		}
	})

	t.Run("limit less than count", func(t *testing.T) {
		names := PodNames(pods, 2)
		if len(names) != 2 {
			t.Errorf("expected 2, got %d", len(names))
		}
		if names[0] != "a" || names[1] != "b" {
			t.Errorf("unexpected names: %v", names)
		}
	})

	t.Run("empty", func(t *testing.T) {
		names := PodNames(nil, 10)
		if len(names) != 0 {
			t.Errorf("expected 0, got %d", len(names))
		}
	})
}

func TestFindPodByName(t *testing.T) {
	now := time.Now()
	pods := []corev1.Pod{
		makePod("pod-a", now, "iaf.io/application", "myapp"),
		makePod("pod-b", now, "iaf.io/application", "myapp"),
	}

	t.Run("found", func(t *testing.T) {
		pod, err := FindPodByName(pods, "pod-a", "iaf.io/application", "myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pod.Name != "pod-a" {
			t.Errorf("expected pod-a, got %s", pod.Name)
		}
	})

	t.Run("not found - wrong name", func(t *testing.T) {
		_, err := FindPodByName(pods, "pod-z", "iaf.io/application", "myapp")
		if err == nil {
			t.Error("expected error for missing pod")
		}
	})

	t.Run("not found - wrong app label", func(t *testing.T) {
		// pod-a belongs to myapp, not otherapp
		_, err := FindPodByName(pods, "pod-a", "iaf.io/application", "otherapp")
		if err == nil {
			t.Error("expected error for wrong app label (cross-app access denied)")
		}
	})
}
