/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/postgres"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

// ManifestImage contains details for a specific image version
type ManifestImage struct {
	ImageKey     string `json:"image-key"`
	ImageName    string `json:"image-name"`
	ImageVersion string `json:"image-version"`
	// remote registry where image is stored
	ImageRemote string `json:"image-remote"`
	// immutable sha version identifier
	ImageDigest string `json:"image-digest"`
	// image tag, exclude with image digest
	ImageTag string `json:"image-tag"`
}

const (
	GlobalHubAgentImageKey       = "multicluster_global_hub_agent"
	GlobalHubManagerImageKey     = "multicluster_global_hub_manager"
	OauthProxyImageKey           = "oauth_proxy"
	GrafanaImageKey              = "grafana"
	PostgresImageKey             = "postgresql"
	GHPostgresDefaultStorageSize = "25Gi"
	// default values for the global hub configured by the operator
	// We may expose these as CRD fields in the future
	AggregationLevel       = "full"
	EnableLocalPolicies    = "true"
	AgentHeartbeatInterval = "60s"
)

var (
	managedClusters    = []string{}
	mghNamespacedName  = types.NamespacedName{}
	oauthSessionSecret = ""
	imageOverrides     = map[string]string{
		GlobalHubAgentImageKey:   "quay.io/stolostron/multicluster-global-hub-agent:latest",
		GlobalHubManagerImageKey: "quay.io/stolostron/multicluster-global-hub-manager:latest",
		OauthProxyImageKey:       "quay.io/stolostron/origin-oauth-proxy:4.9",
		GrafanaImageKey:          "quay.io/stolostron/grafana:globalhub-1.0",
		PostgresImageKey:         "quay.io/stolostron/postgresql-13:1-101",
	}
	statisticLogInterval  = "1m"
	metricsScrapeInterval = "1m"
	imagePullSecretName   = ""
	transporter           transport.Transporter
)

func SetMGHNamespacedName(namespacedName types.NamespacedName) {
	mghNamespacedName = namespacedName
}

func GetMGHNamespacedName() types.NamespacedName {
	return mghNamespacedName
}

func GetOauthSessionSecret() (string, error) {
	if oauthSessionSecret == "" {
		b := make([]byte, 16)
		_, err := rand.Read(b)
		if err != nil {
			return "", err
		}
		oauthSessionSecret = base64.StdEncoding.EncodeToString(b)
	}
	return oauthSessionSecret, nil
}

// getAnnotation returns the annotation value for a given key, or an empty string if not set
func getAnnotation(mgh *globalhubv1alpha4.MulticlusterGlobalHub, annotationKey string) string {
	annotations := mgh.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[annotationKey]
}

// IsPaused returns true if the MulticlusterGlobalHub instance is annotated as paused, and false otherwise
func IsPaused(mgh *globalhubv1alpha4.MulticlusterGlobalHub) bool {
	isPausedVal := getAnnotation(mgh, operatorconstants.AnnotationMGHPause)
	if isPausedVal != "" && strings.EqualFold(isPausedVal, "true") {
		return true
	}

	return false
}

// GetSchedulerInterval returns the scheduler interval for moving policy compliance history
func GetSchedulerInterval(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	return getAnnotation(mgh, operatorconstants.AnnotationMGHSchedulerInterval)
}

// SkipAuth returns true to skip authenticate for non-k8s api
func SkipAuth(mgh *globalhubv1alpha4.MulticlusterGlobalHub) bool {
	toSkipAuth := getAnnotation(mgh, operatorconstants.AnnotationMGHSkipAuth)
	if toSkipAuth != "" && strings.EqualFold(toSkipAuth, "true") {
		return true
	}

	return false
}

func GetInstallCrunchyOperator(mgh *globalhubv1alpha4.MulticlusterGlobalHub) bool {
	toInstallCrunchyOperator := getAnnotation(mgh, operatorconstants.AnnotationMGHInstallCrunchyOperator)
	if toInstallCrunchyOperator != "" && strings.EqualFold(toInstallCrunchyOperator, "true") {
		return true
	}

	return false
}

// GetLaunchJobNames returns the jobs concatenated using "," wchich will run once the constainer is started
func GetLaunchJobNames(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	return getAnnotation(mgh, operatorconstants.AnnotationLaunchJobNames)
}

// GetImageOverridesConfigmap returns the images override configmap annotation, or an empty string if not set
func GetImageOverridesConfigmap(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	return getAnnotation(mgh, operatorconstants.AnnotationImageOverridesCM)
}

func SetImageOverrides(mgh *globalhubv1alpha4.MulticlusterGlobalHub) error {
	// first check for environment variables containing the 'RELATED_IMAGE_' prefix
	for _, env := range os.Environ() {
		envKeyVal := strings.SplitN(env, "=", 2)
		if strings.HasPrefix(envKeyVal[0], operatorconstants.MGHOperandImagePrefix) {
			key := strings.ToLower(strings.Replace(envKeyVal[0],
				operatorconstants.MGHOperandImagePrefix, "", -1))
			imageOverrides[key] = envKeyVal[1]
		}
	}

	// second override image repo
	imageRepoOverride := getAnnotation(mgh, operatorconstants.AnnotationImageRepo)
	if imageRepoOverride != "" {
		for imageKey, imageRef := range imageOverrides {
			imageIndex := strings.LastIndex(imageRef, "/")
			imageOverrides[imageKey] = fmt.Sprintf("%s%s", imageRepoOverride, imageRef[imageIndex:])
		}
	}
	return nil
}

// GetImage is used to retrieve image for given component
func GetImage(componentName string) string {
	return imageOverrides[componentName]
}

// cache the managed clusters
func AppendManagedCluster(name string) {
	for index := range managedClusters {
		if managedClusters[index] == name {
			return
		}
	}
	managedClusters = append(managedClusters, name)
}

func DeleteManagedCluster(name string) {
	for index := range managedClusters {
		if managedClusters[index] == name {
			managedClusters = append(managedClusters[:index], managedClusters[index+1:]...)
			return
		}
	}
}

func GetManagedClusters() []string {
	return managedClusters
}

func SetStatisticLogInterval(mgh *globalhubv1alpha4.MulticlusterGlobalHub) error {
	interval := getAnnotation(mgh, operatorconstants.AnnotationStatisticInterval)
	if interval == "" {
		return nil
	}

	_, err := time.ParseDuration(interval)
	if err != nil {
		return err
	}
	statisticLogInterval = interval
	return nil
}

func GetStatisticLogInterval() string {
	return statisticLogInterval
}

func GetMetricsScrapeInterval(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	interval := getAnnotation(mgh, operatorconstants.AnnotationMetricsScrapeInterval)
	if interval == "" {
		interval = metricsScrapeInterval
	}
	return interval
}

func GetPostgresStorageSize(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	if mgh.Spec.DataLayer.Postgres.StorageSize != "" {
		return mgh.Spec.DataLayer.Postgres.StorageSize
	}
	return GHPostgresDefaultStorageSize
}

func GetKafkaStorageSize(mgh *globalhubv1alpha4.MulticlusterGlobalHub) string {
	defaultKafkaStorageSize := "10Gi"
	if mgh.Spec.DataLayer.Kafka.StorageSize != "" {
		return mgh.Spec.DataLayer.Kafka.StorageSize
	}
	return defaultKafkaStorageSize
}

func SetImagePullSecretName(mgh *globalhubv1alpha4.MulticlusterGlobalHub) {
	if mgh.Spec.ImagePullSecret != imagePullSecretName {
		imagePullSecretName = mgh.Spec.ImagePullSecret
	}
}

func GetImagePullSecretName() string {
	return imagePullSecretName
}

func SetTransporter(p transport.Transporter) {
	transporter = p
}

func GetTransporter() transport.Transporter {
	return transporter
}

// GeneratePGConnectionFromGHStorageSecret returns a postgres connection from the GH storage secret
func GetPGConnectionFromGHStorageSecret(ctx context.Context, client client.Client) (
	*postgres.PostgresConnection, error,
) {
	pgSecret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{
		Name:      constants.GHStorageSecretName,
		Namespace: utils.GetDefaultNamespace(),
	}, pgSecret)
	if err != nil {
		return nil, err
	}
	return &postgres.PostgresConnection{
		SuperuserDatabaseURI:    string(pgSecret.Data["database_uri"]),
		ReadonlyUserDatabaseURI: string(pgSecret.Data["database_uri_with_readonlyuser"]),
		CACert:                  pgSecret.Data["ca.crt"],
	}, nil
}

func GetPGConnectionFromBuildInPostgres(ctx context.Context, client client.Client) (
	*postgres.PostgresConnection, error,
) {
	// wait for postgres guest user secret to be ready
	guestPostgresSecret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{
		Name:      postgres.PostgresGuestUserSecretName,
		Namespace: utils.GetDefaultNamespace(),
	}, guestPostgresSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("postgres guest user secret %s is nil", postgres.PostgresGuestUserSecretName)
		}
		return nil, err
	}
	// wait for postgres super user secret to be ready
	superuserPostgresSecret := &corev1.Secret{}
	err = client.Get(ctx, types.NamespacedName{
		Name:      postgres.PostgresSuperUserSecretName,
		Namespace: utils.GetDefaultNamespace(),
	}, superuserPostgresSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("postgres super user secret %s is nil", postgres.PostgresSuperUserSecretName)
		}
		return nil, err
	}
	// wait for postgres cert secret to be ready
	postgresCertName := &corev1.Secret{}
	err = client.Get(ctx, types.NamespacedName{
		Name:      postgres.PostgresCertName,
		Namespace: utils.GetDefaultNamespace(),
	}, postgresCertName)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("postgres cert secret %s is nil", postgres.PostgresCertName)
		}
		return nil, err
	}

	return &postgres.PostgresConnection{
		SuperuserDatabaseURI:    string(superuserPostgresSecret.Data["uri"]) + postgres.PostgresURIWithSslmode,
		ReadonlyUserDatabaseURI: string(guestPostgresSecret.Data["uri"]) + postgres.PostgresURIWithSslmode,
		CACert:                  postgresCertName.Data["ca.crt"],
	}, nil
}
