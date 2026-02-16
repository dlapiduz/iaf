package k8s

import (
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// KpackImageGVR is the GroupVersionResource for kpack Image CRs.
var KpackImageGVR = schema.GroupVersionResource{
	Group:    "kpack.io",
	Version:  "v1alpha2",
	Resource: "images",
}

// KpackImageGVK is the GroupVersionKind for kpack Image CRs.
var KpackImageGVK = schema.GroupVersionKind{
	Group:   "kpack.io",
	Version: "v1alpha2",
	Kind:    "Image",
}

// BuildKpackImage constructs an unstructured kpack Image CR for the given application.
func BuildKpackImage(app *iafv1alpha1.Application, clusterBuilder, registryPrefix string) *unstructured.Unstructured {
	imageTag := fmt.Sprintf("%s/%s", registryPrefix, app.Name)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(KpackImageGVK)
	obj.SetName(app.Name)
	obj.SetNamespace(app.Namespace)
	obj.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "iaf",
		"iaf.io/application":          app.Name,
	})

	// Set owner reference so the kpack Image is cleaned up with the Application
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: iafv1alpha1.GroupVersion.String(),
			Kind:       "Application",
			Name:       app.Name,
			UID:        app.UID,
		},
	})

	spec := map[string]any{
		"tag": imageTag,
		"builder": map[string]any{
			"name": clusterBuilder,
			"kind": "ClusterBuilder",
		},
		"serviceAccountName": "iaf-kpack-sa",
	}

	// Set source based on Application spec
	if app.Spec.Git != nil {
		revision := app.Spec.Git.Revision
		if revision == "" {
			revision = "main"
		}
		spec["source"] = map[string]any{
			"git": map[string]any{
				"url":      app.Spec.Git.URL,
				"revision": revision,
			},
		}
	} else if app.Spec.Blob != "" {
		spec["source"] = map[string]any{
			"blob": map[string]any{
				"url": app.Spec.Blob,
			},
		}
	}

	obj.Object["spec"] = spec
	return obj
}

// GetKpackImageStatus extracts build status information from a kpack Image CR.
func GetKpackImageStatus(obj *unstructured.Unstructured) (buildStatus string, latestImage string) {
	status, ok := obj.Object["status"].(map[string]any)
	if !ok {
		return "Unknown", ""
	}

	// Get latest image
	if img, ok := status["latestImage"].(string); ok {
		latestImage = img
	}

	// Check conditions for build status
	conditions, ok := status["conditions"].([]any)
	if !ok {
		return "Unknown", latestImage
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)

		if condType == "Ready" {
			switch condStatus {
			case "True":
				return "Succeeded", latestImage
			case "False":
				return "Failed", latestImage
			default:
				return "Building", latestImage
			}
		}
	}

	return "Building", latestImage
}
