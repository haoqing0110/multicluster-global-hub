package generic

import (
	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
)

// NewBundleEntry creates a new instance of BundleCollectionEntry.
func NewBundleEntry(transportBundleKey string, bundle bundle.AgentBundle,
	bundlePredicate func() bool,
) *BundleEntry {
	return &BundleEntry{
		transportBundleKey:    transportBundleKey,
		bundle:                bundle,
		bundlePredicate:       bundlePredicate,
		lastSentBundleVersion: *bundle.GetVersion(),
	}
}

// BundleEntry holds information about a specific bundle.
type BundleEntry struct {
	transportBundleKey    string
	bundle                bundle.AgentBundle
	bundlePredicate       func() bool
	lastSentBundleVersion metadata.BundleVersion // not pointer so it does not point to the bundle's internal version
}

func NewSharedBundleEntry(transportBundleKey string, baseAgentBundle bundle.BaseAgentBundle,
	bundlePredicate func() bool,
) *SharedBundleEntry {
	return &SharedBundleEntry{
		transportBundleKey:    transportBundleKey,
		bundle:                baseAgentBundle,
		bundlePredicate:       bundlePredicate,
		lastSentBundleVersion: *baseAgentBundle.GetVersion(),
	}
}

type SharedBundleEntry struct {
	transportBundleKey    string
	bundle                bundle.BaseAgentBundle
	bundlePredicate       func() bool
	lastSentBundleVersion metadata.BundleVersion // not pointer so it does not point to the bundle's internal version
}
