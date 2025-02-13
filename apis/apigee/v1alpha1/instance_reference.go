// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"context"
	"fmt"

	refsv1beta1 "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/k8s"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ refsv1beta1.RefNormalizer = &InstanceRef{}

// InstanceRef is a reference to a ApigeeInstance resource.
type InstanceRef struct {
	// A reference to an externally managed ApigeeInstance resource.
	// Should be in the format "organizations/{{organizationID}}/instances/{{instanceID}}".
	External string `json:"external,omitempty"`

	// The name of a ApigeeInstance resource.
	Name string `json:"name,omitempty"`

	// The namespace of a ApigeeInstance resource.
	Namespace string `json:"namespace,omitempty"`
}

// Normalize ensures the "External" field is specified for a given InstanceRef,
// and that it has the correct format.
//
// If "External" field is already specified, the format will be validated.
//
// If "External" field is not specified, the "Name" and "Namespace" fields will
// be used to query the referenced object from the K8s API and fetch it's
// external reference value. If "Namespace" field is not specified, the
// `defaultNamespace“ will be used instead.
func (r *InstanceRef) Normalize(ctx context.Context, reader client.Reader, defaultNamespace string) error {
	if r.External == "" {
		if r.Namespace == "" {
			r.Namespace = defaultNamespace
		}
		key := types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(ApigeeInstanceGVK)
		if err := reader.Get(ctx, key, u); err != nil {
			if apierrors.IsNotFound(err) {
				return k8s.NewReferenceNotFoundError(u.GroupVersionKind(), key)
			}
			return fmt.Errorf("reading referenced %s %s: %w", ApigeeInstanceGVK, key, err)
		}
		// Get external from status.externalRef. This is the most trustworthy place.
		externalRef, _, err := unstructured.NestedString(u.Object, "status", "externalRef")
		if err != nil {
			return fmt.Errorf("reading status.externalRef: %w", err)
		}
		if externalRef == "" {
			return k8s.NewReferenceNotReadyError(u.GroupVersionKind(), key)
		}
		r.External = externalRef
	}

	if _, err := NewInstanceIdentity(r.External); err != nil {
		return err
	}
	return nil
}
