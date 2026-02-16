package tools

import (
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Dependencies holds shared dependencies for MCP tools.
type Dependencies struct {
	Client     client.Client
	Namespace  string
	Store      *sourcestore.Store
	BaseDomain string
}
