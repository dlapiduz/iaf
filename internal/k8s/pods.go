package k8s

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

const MaxAvailablePods = 10

// SelectMostRecentPod returns the pod with the most recent CreationTimestamp.
// Returns nil if the slice is empty.
func SelectMostRecentPod(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}
	sorted := make([]corev1.Pod, len(pods))
	copy(sorted, pods)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreationTimestamp.After(sorted[j].CreationTimestamp.Time)
	})
	return &sorted[0]
}

// PodNames returns the names of up to limit pods.
func PodNames(pods []corev1.Pod, limit int) []string {
	cap := limit
	if len(pods) < cap {
		cap = len(pods)
	}
	names := make([]string, 0, cap)
	for i, pod := range pods {
		if i >= limit {
			break
		}
		names = append(names, pod.Name)
	}
	return names
}

// FindPodByName finds a pod by name and validates it belongs to the app via label.
// Returns an error if the pod is not found in the list (prevents cross-app log access).
func FindPodByName(pods []corev1.Pod, podName, labelKey, labelValue string) (*corev1.Pod, error) {
	for i := range pods {
		pod := &pods[i]
		if pod.Name == podName && pod.Labels[labelKey] == labelValue {
			return pod, nil
		}
	}
	return nil, fmt.Errorf("pod %q not found for application %q", podName, labelValue)
}

