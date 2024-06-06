// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spanner

import (
	"context"
	"fmt"

	databaseapi "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	"google.golang.org/api/option"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// kmskrm "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/kms/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/spanner/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/k8s"
)

const ctlrName = "spannerdatabase-controller"

func init() {
	registry.RegisterModel(krm.SpannerDatabaseGVK, NewSpannerDatabaseModel)
}

type spannerDatabaseModel struct {
	config *config.ControllerConfig
}

var _ directbase.Model = &spannerDatabaseModel{}

func NewSpannerDatabaseModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &spannerDatabaseModel{config: config}, nil
}

var _ directbase.Adapter = &spannerDatabaseAdapter{}

type spannerDatabaseAdapter struct {
	projectID    string
	instanceID   string
	databaseID   string
	cryptoKeyFQN string

	desired *krm.SpannerDatabase

	dbClient *databaseapi.DatabaseAdminClient
}

func (m *spannerDatabaseModel) client(ctx context.Context) (*databaseapi.DatabaseAdminClient, error) {
	var opts []option.ClientOption
	if m.config.UserAgent != "" {
		opts = append(opts, option.WithUserAgent(m.config.UserAgent))
	}
	if m.config.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(m.config.HTTPClient))
	}
	if m.config.UserProjectOverride && m.config.BillingProject != "" {
		opts = append(opts, option.WithQuotaProject(m.config.BillingProject))
	}

	gcpClient, err := databaseapi.NewDatabaseAdminRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SpannerDatabase client: %w", err)
	}
	return gcpClient, err
}

// AdapterForObject implements the Model interface.
func (m *spannerDatabaseModel) AdapterForObject(ctx context.Context, reader client.Reader, u *unstructured.Unstructured) (directbase.Adapter, error) {
	client, err := m.client(ctx)
	if err != nil {
		return nil, err
	}

	obj := &krm.SpannerDatabase{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}

	// TODO: Resolve external
	instanceID := obj.Spec.InstanceRef.Name

	// TODO(yuwenma): following current behavior. But do we have better option?
	databaseID := directbase.ValueOf(obj.Spec.ResourceID)
	if databaseID == "" {
		databaseID = obj.GetName()
	}

	// TODO(yuwenma): following current behavior. But do we have better option?
	projectID, ok := u.GetAnnotations()[k8s.ProjectIDAnnotation]
	if !ok {
		projectID = u.GetNamespace()
	}

	cryptoKeyFQN := ""
	if obj.Spec.EncryptionConfig != nil {
		cryptoKeyFQN, err = resolveCryptoKeyFQN(ctx, reader, obj, projectID)
		if err != nil {
			return nil, err
		}
	}

	return &spannerDatabaseAdapter{
		projectID:    projectID,
		instanceID:   instanceID,
		databaseID:   databaseID,
		cryptoKeyFQN: cryptoKeyFQN,
		desired:      obj,
		dbClient:     client,
	}, nil
}

// Find implements the Adapter interface.
func (a *spannerDatabaseAdapter) Find(ctx context.Context) (bool, error) {
	if a.databaseID == "" {
		return false, nil
	}

	req := &databasepb.GetDatabaseRequest{
		Name: a.fullyQualifiedName(),
	}
	_, err := a.dbClient.GetDatabase(ctx, req)
	if err != nil {
		if directbase.IsNotFound(err) {
			klog.Warningf("SpannerDatabase was not found: %v", err)
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// Delete implements the Adapter interface.
func (a *spannerDatabaseAdapter) Delete(ctx context.Context) (bool, error) {
	// TODO: Delete via status selfLink
	req := &databasepb.DropDatabaseRequest{
		Database: a.fullyQualifiedName(),
	}
	if err := a.dbClient.DropDatabase(ctx, req); err != nil {
		if directbase.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting key: %w", err)
	}
	return true, nil
}

// Create implements the Adapter interface.
func (a *spannerDatabaseAdapter) Create(ctx context.Context, u *unstructured.Unstructured) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating object", "u", u)

	req, err := a.spannerDatabaseKRMToCreateDatabaseRequest(a.desired)
	if err != nil {
		return fmt.Errorf("convert SpannerDatabase KRM to CreateDatabaseRequest API: %w", err)
	}

	log.Info("creating spannerDatabase", "spannerDatabase", req)

	op, err := a.dbClient.CreateDatabase(ctx, req)
	if err != nil {
		return fmt.Errorf("creating spannerDatabase: %w", err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting for spannerDatabase creation: %w", err)
	}
	log.V(2).Info("created spannerDatabase", "spannerDatabase", created)

	return nil
}

func (a *spannerDatabaseAdapter) Update(ctx context.Context, u *unstructured.Unstructured) error {
	// TODO
	return nil
}

func (a *spannerDatabaseAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (a *spannerDatabaseAdapter) fullyQualifiedName() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", a.projectID, a.instanceID, a.databaseID)
}

func (a *spannerDatabaseAdapter) spannerDatabaseKRMToCreateDatabaseRequest(r *krm.SpannerDatabase) (*databasepb.CreateDatabaseRequest, error) {
	// Default database dialect is GOOGLE_STANDARD_SQL
	databaseDialect := databasepb.DatabaseDialect_DATABASE_DIALECT_UNSPECIFIED
	if r.Spec.DatabaseDialect != nil {
		if directbase.ValueOf(r.Spec.DatabaseDialect) == "GOOGLE_STANDARD_SQL" {
			databaseDialect = databasepb.DatabaseDialect_GOOGLE_STANDARD_SQL
		} else if directbase.ValueOf(r.Spec.DatabaseDialect) == "POSTGRESQL" {
			databaseDialect = databasepb.DatabaseDialect_POSTGRESQL
		} else {
			return nil, fmt.Errorf("unsupported database dialect: %s", directbase.ValueOf(r.Spec.DatabaseDialect))
		}
	}

	// For POSTGRESQL, database name must be enclosed in double quotes
	createDelimiter := '`'
	if databaseDialect == databasepb.DatabaseDialect_POSTGRESQL {
		createDelimiter = '"'
	}
	createStatement := fmt.Sprintf(
		"CREATE DATABASE %c%s%c",
		createDelimiter,
		a.databaseID,
		createDelimiter,
	)

	// Add version retention period if specified
	extraStatements := r.Spec.Ddl
	if r.Spec.VersionRetentionPeriod != nil {
		extraStatements = append(extraStatements, fmt.Sprintf(
			"ALTER DATABASE %c%s%c SET OPTIONS (version_retention_period = '%s')",
			createDelimiter,
			a.databaseID,
			createDelimiter,
			directbase.ValueOf(r.Spec.VersionRetentionPeriod)),
		)
	}

	// Set encryption config if specified
	var encryptionConfig *databasepb.EncryptionConfig
	if r.Spec.EncryptionConfig != nil {
		encryptionConfig = &databasepb.EncryptionConfig{
			KmsKeyName: a.cryptoKeyFQN,
		}
	}

	return &databasepb.CreateDatabaseRequest{
		Parent:           fmt.Sprintf("projects/%s/instances/%s", a.projectID, a.instanceID),
		CreateStatement:  createStatement,
		ExtraStatements:  extraStatements,
		DatabaseDialect:  databaseDialect,
		EncryptionConfig: encryptionConfig,
	}, nil
}

func resolveCryptoKeyFQN(ctx context.Context, reader client.Reader, obj *krm.SpannerDatabase, projectID string) (string, error) {
	/*
	   	Note, this code does not work. It appears that the KMSCryptoKey and KMSKeyRing kinds are not added
	   	to the scheme. When running this code, the following error is produced:

	   	{"severity":"error","timestamp":"2024-06-06T17:04:06.411Z","msg":"Reconciler error","controller":"spannerdatabase-controller","controllerGroup":"spanner.cnrm.cloud.google.com","controllerKind":"SpannerDatabase","SpannerDatabase":{"name":"spannerdatabase-test","namespace":"default"},"namespace":"default","name":"spannerdatabase-test","reconcileID":"6719ee95-fad2-4a59-8ec8-18df975d7ce9","erro
	   r":"Update call failed: error reading referenced KMSCryptoKey default/kmscryptokey-test: no kind is registered for the type v1beta1.KMSCryptoKey in scheme \"pkg/runtime/scheme.go:100\""}

	   	cryptoKeyKey := types.NamespacedName{
	   		Namespace: obj.Spec.EncryptionConfig.KmsKeyRef.Namespace,
	   		Name:      obj.Spec.EncryptionConfig.KmsKeyRef.Name,
	   	}
	   	if cryptoKeyKey.Namespace == "" {
	   		cryptoKeyKey.Namespace = u.GetNamespace()
	   	}
	   	cryptoKey := kmskrm.KMSCryptoKey{}
	   	if err := reader.Get(ctx, cryptoKeyKey, &cryptoKey); err != nil {
	   		if apierrors.IsNotFound(err) {
	   			return nil, fmt.Errorf("referenced KMSCryptoKey %v not found", cryptoKeyKey)
	   		}
	   		return nil, fmt.Errorf("error reading referenced KMSCryptoKey %v: %w", cryptoKeyKey, err)
	   	}
	   	keyringKey := types.NamespacedName{
	   		Namespace: cryptoKey.Spec.KeyRingRef.Namespace,
	   		Name:      cryptoKey.Spec.KeyRingRef.Name,
	   	}
	   	if keyringKey.Namespace == "" {
	   		keyringKey.Namespace = u.GetNamespace()
	   	}
	   	keyring := kmskrm.KMSKeyRing{}
	   	if err := reader.Get(ctx, keyringKey, &keyring); err != nil {
	   		if apierrors.IsNotFound(err) {
	   			return nil, fmt.Errorf("referenced KMSKeyRing %v not found", keyringKey)
	   		}
	   		return nil, fmt.Errorf("error reading referenced KMSKeyRing %v: %w", keyringKey, err)
	   	}
	*/

	/*
		The following code works, but it's a bit cumbersome. Why do the KMSCryptoKey and KMSKeyRing need to be fetched
		as unstructured types? For example, as done in pkg/controller/direct/references/projectref.go. Why aren't the
		types for the CRDs added to our controller manager scheme?
	*/

	cryptoKeyNamespace := obj.Spec.EncryptionConfig.KmsKeyRef.Namespace
	cryptoKeyName := obj.Spec.EncryptionConfig.KmsKeyRef.Name
	cryptoKeyKey := types.NamespacedName{
		Namespace: cryptoKeyNamespace,
		Name:      cryptoKeyName,
	}
	if cryptoKeyKey.Namespace == "" {
		cryptoKeyKey.Namespace = obj.GetNamespace()
	}
	cryptoKey := &unstructured.Unstructured{}
	cryptoKey.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kms.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "KMSCryptoKey",
	})
	if err := reader.Get(ctx, cryptoKeyKey, cryptoKey); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("referenced KMSCryptoKey %v not found", cryptoKeyKey)
		}
		return "", fmt.Errorf("error reading referenced KMSCryptoKey %v: %w", cryptoKeyKey, err)
	}

	keyRingName, _, err := unstructured.NestedString(cryptoKey.Object, "spec", "keyRingRef", "name")
	if err != nil {
		return "", fmt.Errorf("reading spec.keyRingRef.name from KMSCryptoKey %v: %w", cryptoKeyKey, err)
	}
	keyRingNamespace, _, err := unstructured.NestedString(cryptoKey.Object, "spec", "keyRingRef", "namespace")
	if err != nil {
		return "", fmt.Errorf("reading spec.keyRingRef.namespace from KMSCryptoKey %v: %w", cryptoKeyKey, err)
	}
	keyRingKey := types.NamespacedName{
		Namespace: keyRingNamespace,
		Name:      keyRingName,
	}
	if keyRingKey.Namespace == "" {
		keyRingKey.Namespace = obj.GetNamespace()
	}

	keyRing := &unstructured.Unstructured{}
	keyRing.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kms.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "KMSKeyRing",
	})
	if err := reader.Get(ctx, keyRingKey, keyRing); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("referenced KMSKeyRing %v not found", keyRingKey)
		}
		return "", fmt.Errorf("error reading referenced KMSKeyRing %v: %w", keyRingKey, err)
	}

	keyRingLocation, _, err := unstructured.NestedString(keyRing.Object, "spec", "location")
	if err != nil {
		return "", fmt.Errorf("reading spec.location from KMSKeyRing %v: %w", keyRingKey, err)
	}

	return fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s",
		projectID,
		keyRingLocation,
		keyRingName,
		cryptoKeyName), nil
}
