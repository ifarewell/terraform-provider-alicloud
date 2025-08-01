package alicloud

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"

	"github.com/alibabacloud-go/tea/tea"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"

	"encoding/base64"

	"encoding/json"

	"github.com/alibabacloud-go/cs-20151215/v5/client"
	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/denverdino/aliyungo/cs"
)

type CsService struct {
	client *connectivity.AliyunClient
}

type CsClient struct {
	client *client.Client
}

type Component struct {
	ComponentName    string      `json:"component_name"`
	Version          string      `json:"version"`
	NextVersion      string      `json:"next_version"`
	CanUpgrade       bool        `json:"can_upgrade"`
	Required         bool        `json:"required"`
	Status           string      `json:"status"`
	ErrMessage       string      `json:"err_message"`
	Config           string      `json:"config"`
	ConfigSchema     string      `json:"config_schema"`
	Error            interface{} `json:"error"`
	SupportedActions []string    `json:"supported_actions"`
}

const (
	COMPONENT_AUTO_SCALER      = "cluster-autoscaler"
	COMPONENT_DEFAULT_VRESION  = "v1.0.0"
	SCALING_CONFIGURATION_NAME = "kubernetes_autoscaler_autogen"
	DefaultECSTag              = "k8s.aliyun.com"
	DefaultClusterTag          = "ack.aliyun.com"
	CsPlayerAccountIdTag       = "ack.playeraccount"
	RECYCLE_MODE_LABEL         = "k8s.io/cluster-autoscaler/node-template/label/policy"
	DefaultAutoscalerTag       = "k8s.io/cluster-autoscaler"
	SCALING_GROUP_NAME         = "sg-%s-%s"
	DEFAULT_COOL_DOWN_TIME     = 300
	RELEASE_MODE               = "release"
	RECYCLE_MODE               = "recycle"

	PRIORITY_POLICY       = "PRIORITY"
	COST_OPTIMIZED_POLICY = "COST_OPTIMIZED"
	BALANCE_POLICY        = "BALANCE"

	UpgradeClusterTimeout = 30 * time.Minute

	IdMsgWithTask = IdMsg + "TaskInfo: %s" // wait for async task info
)

var (
	ATTACH_SCRIPT_WITH_VERSION = `#!/bin/sh
curl http://aliacs-k8s-%s.oss-%s.aliyuncs.com/public/pkg/run/attach/%s/attach_node.sh | bash -s -- --openapi-token %s --ess true `
	NETWORK_ADDON_NAMES = []string{"terway", "kube-flannel-ds", "terway-eni", "terway-eniip"}
)

func (s *CsService) GetContainerClusterByName(name string) (cluster cs.ClusterType, err error) {
	name = Trim(name)
	invoker := NewInvoker()
	var clusters []cs.ClusterType
	err = invoker.Run(func() error {
		raw, e := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			return csClient.DescribeClusters(name)
		})
		if e != nil {
			return e
		}
		clusters, _ = raw.([]cs.ClusterType)
		return nil
	})

	if err != nil {
		return cluster, fmt.Errorf("Describe cluster failed by name %s: %#v.", name, err)
	}

	if len(clusters) < 1 {
		return cluster, GetNotFoundErrorFromString(GetNotFoundMessage("Container Cluster", name))
	}

	for _, c := range clusters {
		if c.Name == name {
			return c, nil
		}
	}
	return cluster, GetNotFoundErrorFromString(GetNotFoundMessage("Container Cluster", name))
}

func (s *CsService) GetContainerClusterAndCertsByName(name string) (*cs.ClusterType, *cs.ClusterCerts, error) {
	cluster, err := s.GetContainerClusterByName(name)
	if err != nil {
		return nil, nil, err
	}
	var certs cs.ClusterCerts
	invoker := NewInvoker()
	err = invoker.Run(func() error {
		raw, e := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			return csClient.GetClusterCerts(cluster.ClusterID)
		})
		if e != nil {
			return e
		}
		certs, _ = raw.(cs.ClusterCerts)
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return &cluster, &certs, nil
}

func (s *CsService) DescribeContainerApplication(clusterName, appName string) (app cs.GetProjectResponse, err error) {
	appName = Trim(appName)
	cluster, certs, err := s.GetContainerClusterAndCertsByName(clusterName)
	if err != nil {
		return app, err
	}
	raw, err := s.client.WithCsProjectClient(cluster.ClusterID, cluster.MasterURL, *certs, func(csProjectClient *cs.ProjectClient) (interface{}, error) {
		return csProjectClient.GetProject(appName)
	})
	app, _ = raw.(cs.GetProjectResponse)
	if err != nil {
		if IsExpectedErrors(err, []string{"Not Found"}) {
			return app, GetNotFoundErrorFromString(GetNotFoundMessage("Container Application", appName))
		}
		return app, fmt.Errorf("Getting Application failed by name %s: %#v.", appName, err)
	}
	if app.Name != appName {
		return app, GetNotFoundErrorFromString(GetNotFoundMessage("Container Application", appName))
	}
	return
}

func (s *CsService) WaitForContainerApplication(clusterName, appName string, status Status, timeout int) error {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	for {
		app, err := s.DescribeContainerApplication(clusterName, appName)
		if err != nil {
			return err
		}

		if strings.ToLower(app.CurrentState) == strings.ToLower(string(status)) {
			break
		}
		timeout = timeout - DefaultIntervalShort
		if timeout <= 0 {
			return GetTimeErrorFromString(fmt.Sprintf("Waitting for container application %s is timeout and current status is %s.", string(status), app.CurrentState))
		}
		time.Sleep(DefaultIntervalShort * time.Second)
	}
	return nil
}

func (s *CsService) DescribeCsKubernetes(id string) (cluster *cs.KubernetesClusterDetail, err error) {
	invoker := NewInvoker()
	var requestInfo *cs.Client
	var response interface{}

	if err := invoker.Run(func() error {
		raw, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			requestInfo = csClient
			return csClient.DescribeKubernetesClusterDetail(id)
		})
		response = raw
		return err
	}); err != nil {
		if IsExpectedErrors(err, []string{"ErrorClusterNotFound"}) {
			return cluster, WrapErrorf(err, NotFoundMsg, DenverdinoAliyungo)
		}
		return cluster, WrapErrorf(err, DefaultErrorMsg, id, "DescribeKubernetesCluster", DenverdinoAliyungo)
	}
	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["ClusterId"] = id
		addDebug("DescribeKubernetesCluster", response, requestInfo, requestMap)
	}
	cluster, _ = response.(*cs.KubernetesClusterDetail)
	if cluster.ClusterId != id {
		return cluster, WrapErrorf(NotFoundErr("CsKubernetes", id), NotFoundMsg, ProviderERROR)
	}
	return
}

func (s *CsClient) DescribeClusterDetail(id string) (*client.DescribeClusterDetailResponseBody, error) {
	if id == "" {
		return nil, WrapError(fmt.Errorf("cluster id is empty"))
	}

	var err error
	var response *client.DescribeClusterDetailResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		response, err = s.client.DescribeClusterDetail(tea.String(id))
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapError(err)
	}

	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["ClusterId"] = id
		addDebug("DescribeClusterDetail", response, id, requestMap)
	}
	return response.Body, nil
}

// DescribeClusterKubeConfig return cluster kube_config credential.
// It's used for kubernetes/managed_kubernetes/serverless_kubernetes.
// Deprecated, use CsClient.DescribeClusterKubeConfigWithExpiration
func (s *CsService) DescribeClusterKubeConfig(clusterId string, isResource bool) (*cs.ClusterConfig, error) {
	invoker := NewInvoker()
	var response interface{}
	var requestInfo *cs.Client
	var config *cs.ClusterConfig
	if err := invoker.Run(func() error {
		raw, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			requestInfo = csClient
			return csClient.DescribeClusterUserConfig(clusterId, false)
		})
		response = raw
		return err
	}); err != nil {
		if isResource {
			return nil, WrapErrorf(err, DefaultErrorMsg, clusterId, "DescribeClusterUserConfig", DenverdinoAliyungo)
		}
		return nil, WrapErrorf(err, DataDefaultErrorMsg, clusterId, "DescribeClusterUserConfig", DenverdinoAliyungo)
	}
	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["Id"] = clusterId
		addDebug("DescribeClusterUserConfig", response, requestInfo, requestMap)
	}
	config, _ = response.(*cs.ClusterConfig)
	return config, nil
}

// DescribeClusterKubeConfigWithExpiration return cluster kube_config credential with expiration time.
// It's used for kubernetes/managed_kubernetes/serverless_kubernetes.
func (s *CsClient) DescribeClusterKubeConfigWithExpiration(clusterId string, temporaryDurationMinutes int64) (*client.DescribeClusterUserKubeconfigResponseBody, error) {
	if clusterId == "" {
		return nil, WrapError(fmt.Errorf("clusterid is empty"))
	}

	request := &client.DescribeClusterUserKubeconfigRequest{
		PrivateIpAddress: tea.Bool(false),
	}
	if temporaryDurationMinutes > 0 {
		request.TemporaryDurationMinutes = tea.Int64(temporaryDurationMinutes)
	}
	var err error
	var kubeConfig *client.DescribeClusterUserKubeconfigResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		kubeConfig, err = s.client.DescribeClusterUserKubeconfig(tea.String(clusterId), request)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapError(err)
	}

	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["ClusterId"] = clusterId
		addDebug("DescribeClusterUserConfig", kubeConfig, request, requestMap)
	}

	return kubeConfig.Body, nil
}

// This function returns all available addons metadata of the cluster
func (s *CsClient) DescribeClusterAddonsMetadata(clusterId string) (map[string]*Component, error) {
	result := make(map[string]*Component)

	var err error
	var resp *client.DescribeClusterAddonsVersionResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.DescribeClusterAddonsVersion(&clusterId)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeClusterAddonsVersion", err)
	}

	for name, addon := range resp.Body {
		c := &Component{}
		bytes, err := json.Marshal(addon)
		if err != nil {
			continue
		}
		err = json.Unmarshal(bytes, c)
		if err != nil {
			continue
		}
		result[name] = c
	}

	return result, nil
}

// This function returns the latest addon status information
func (s *CsClient) DescribeCsKubernetesAddonStatus(clusterId string, addonName string) (*Component, error) {
	result := &Component{}
	var err error
	var resp *client.DescribeClusterAddonsUpgradeStatusResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.DescribeClusterAddonsUpgradeStatus(&clusterId, &client.DescribeClusterAddonsUpgradeStatusRequest{
			ComponentIds: []*string{tea.String(addonName)},
		})
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeClusterAddonsUpgradeStatus", err)
	}

	addon, ok := resp.Body[addonName]
	if !ok {
		return nil, WrapErrorf(NotFoundErr("alicloud_cs_kubernetes_addon", addonName), ResourceNotfound)
	}

	addonInfo := addon.(map[string]interface{})["addon_info"]
	tasks := addon.(map[string]interface{})["tasks"]
	result.Version = addonInfo.(map[string]interface{})["version"].(string)
	result.CanUpgrade = addon.(map[string]interface{})["can_upgrade"].(bool)
	result.Status = tasks.(map[string]interface{})["status"].(string)
	if tErr, ok := tasks.(map[string]interface{})["error"]; ok {
		result.Error = tErr
	}
	if message, ok := tasks.(map[string]interface{})["message"]; ok {
		result.ErrMessage = message.(string)
	}

	return result, nil
}

// This function returns the latest addon instance
func (s *CsClient) DescribeCsKubernetesAddonInstance(clusterId string, addonName string) (*Component, error) {
	component := &Component{}
	var err error
	var resp *client.DescribeClusterAddonInstanceResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.DescribeClusterAddonInstance(&clusterId, tea.String(addonName))
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"AddonNotFound"}) {
			err = WrapErrorf(NotFoundErr("alicloud_cs_kubernetes_addon", addonName), ResourceNotfound)
			return component, err
		}
		return nil, err
	}

	// FixMe: Currently, the addon does not support the initial state and needs to be returned in a task state.
	if resp.Body.State == nil || *resp.Body.State == "" {
		result, err := s.DescribeCsKubernetesAddonStatus(clusterId, addonName)
		return result, err
	}

	if resp.Body.Name != nil {
		component.ComponentName = *resp.Body.Name
	}
	if resp.Body.Version != nil {
		component.Version = *resp.Body.Version
	}
	if resp.Body.State != nil {
		component.Status = *resp.Body.State
	}

	if resp.Body.Config != nil {
		component.Config = *resp.Body.Config
	}

	return component, nil
}

func (s *CsClient) GetCsKubernetesAddonInstance(clusterId string, addonName string) (*Component, error) {
	component := &Component{}
	var err error
	var resp *client.GetClusterAddonInstanceResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.GetClusterAddonInstance(&clusterId, tea.String(addonName))
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"AddonNotFound"}) {
			err = WrapErrorf(NotFoundErr("alicloud_cs_kubernetes_addon", addonName), ResourceNotfound)
			return component, err
		}
		return nil, err
	}

	if resp.Body.Name != nil {
		component.ComponentName = *resp.Body.Name
	}
	if resp.Body.Version != nil {
		component.Version = *resp.Body.Version
	}
	if resp.Body.State != nil {
		component.Status = *resp.Body.State
	}

	if resp.Body.Config != nil {
		component.Config = *resp.Body.Config
	}

	return component, nil
}

// This function returns the status of all available addons of the cluster
func (s *CsClient) DescribeCsKubernetesAllAvailableAddons(clusterId string) (map[string]*Component, error) {
	availableAddons, err := s.DescribeClusterAddonsMetadata(clusterId)
	if err != nil {
		return nil, err
	}

	queryList := make([]*string, 0)
	for name := range availableAddons {
		queryList = append(queryList, tea.String(name))
	}

	status, err := s.DescribeCsKubernetesAllAddonsStatus(clusterId, queryList)
	if err != nil {
		return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeCsKubernetesExistedAddons", err)
	}

	for name, addon := range availableAddons {
		if _, ok := status[name]; !ok {
			continue
		}
		addon.Version = status[name].Version
		addon.CanUpgrade = status[name].CanUpgrade
		addon.ErrMessage = status[name].ErrMessage
		addon.Config = status[name].Config
		if addon.Version == "" {
			continue
		}
		addonInstance, err := s.DescribeCsKubernetesAddonInstance(clusterId, name)
		if err != nil {
			if NotFoundError(err) {
				continue
			}
			return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeCsKubernetesExistedAddons", err)
		}
		addon.Config = addonInstance.Config
		addon.Status = addonInstance.Status
	}

	return availableAddons, nil
}

// This function returns the latest multiple addons status information
func (s *CsClient) DescribeCsKubernetesAllAddonsStatus(clusterId string, addons []*string) (map[string]*Component, error) {
	addonsStatus := make(map[string]*Component)
	var err error
	var resp *client.DescribeClusterAddonsUpgradeStatusResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.DescribeClusterAddonsUpgradeStatus(&clusterId, &client.DescribeClusterAddonsUpgradeStatusRequest{
			ComponentIds: addons,
		})
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeClusterAddonsUpgradeStatus", err)
	}

	for name, status := range resp.Body {
		c := &Component{}
		addonInfo := status.(map[string]interface{})["addon_info"]
		tasks := status.(map[string]interface{})["tasks"]
		c.Version = addonInfo.(map[string]interface{})["version"].(string)
		c.CanUpgrade = status.(map[string]interface{})["can_upgrade"].(bool)
		c.Status = tasks.(map[string]interface{})["status"].(string)
		if message, ok := tasks.(map[string]interface{})["message"]; ok {
			c.ErrMessage = message.(string)
		}

		addonsStatus[name] = c
	}

	return addonsStatus, nil
}

func (s *CsClient) DescribeCsKubernetesAddon(id string) (*Component, error) {
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return nil, WrapError(err)
	}
	clusterId := parts[0]
	addonName := parts[1]

	addonInstance, err := s.DescribeCsKubernetesAddonInstance(clusterId, addonName)
	if err != nil {
		if IsExpectedErrors(err, []string{"AddonNotFound", "ErrorClusterNotFound"}) || NotFoundError(err) {
			return nil, WrapErrorf(NotFoundErr("alicloud_cs_kubernetes_addon", id), ResourceNotfound)
		}
		return nil, err
	}

	addonsMetadata, err := s.DescribeClusterAddonsMetadata(clusterId)
	if err != nil {
		return nil, err
	}

	addonStatus, err := s.DescribeCsKubernetesAddonStatus(clusterId, addonName)
	if err != nil {
		return nil, err
	}

	// Update some fields
	if addon, existed := addonsMetadata[addonName]; existed {
		addon.Version = addonStatus.Version
		addon.Status = addonStatus.Status
		addon.CanUpgrade = addonStatus.CanUpgrade
		addon.ErrMessage = addonStatus.ErrMessage
		addon.Config = addonInstance.Config
		return addon, nil
	}

	return nil, WrapErrorf(NotFoundErr("alicloud_cs_kubernetes_addon", id), ResourceNotfound)
}

func (s *CsClient) CsKubernetesAddonTaskRefreshFunc(clusterId string, addonName string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsKubernetesAddonStatus(clusterId, addonName)
		if err != nil {
			if NotFoundError(err) {
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}
		for _, failState := range failStates {
			if object.Status == failState {
				return object, object.Status, WrapError(Error(FailedToReachTargetStatusWithResponse, clusterId, object.ErrMessage))
			}
		}
		return object, object.Status, nil
	}
}

func (s *CsClient) CsKubernetesAddonStateRefreshFunc(clusterId string, addonName string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsKubernetesAddonInstance(clusterId, addonName)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}
		for _, failState := range failStates {
			if object.Status == failState {
				return object, object.Status, WrapError(Error(FailedToReachTargetStatusWithResponse, clusterId, object.ErrMessage))
			}
		}
		return object, object.Status, nil
	}
}

func (s *CsClient) CsKubernetesAddonExistRefreshFunc(clusterId string, addonName string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsKubernetesAddonInstance(clusterId, addonName)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return object, "deleted", nil
			}
			return nil, "", WrapError(err)
		}

		return object, object.Status, nil
	}
}
func (s *CsClient) DescribeCsAutoscalingConfig(id string) (*client.CreateAutoscalingConfigRequest, error) {
	request := &client.CreateAutoscalingConfigRequest{
		CoolDownDuration:        tea.String("10m"),
		UnneededDuration:        tea.String("10m"),
		UtilizationThreshold:    tea.String("0.5"),
		GpuUtilizationThreshold: tea.String("0.5"),
		ScanInterval:            tea.String("30s"),
	}
	return request, nil
}

func (s *CsClient) installAddon(d *schema.ResourceData) error {
	clusterId := d.Get("cluster_id").(string)

	body := make([]*client.InstallClusterAddonsRequestBody, 0)
	b := &client.InstallClusterAddonsRequestBody{
		Name:    tea.String(d.Get("name").(string)),
		Version: tea.String(d.Get("version").(string)),
	}

	if config, exist := d.GetOk("config"); exist {
		b.Config = tea.String(config.(string))
	}
	body = append(body, b)

	creationArgs := &client.InstallClusterAddonsRequest{
		Body: body,
	}

	var err error
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		_, err = s.client.InstallClusterAddons(&clusterId, creationArgs)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "installAddon", err)
	}

	return nil
}

func (s *CsClient) upgradeAddon(d *schema.ResourceData, updateVersion, updateConfig bool) error {
	clusterId := d.Get("cluster_id").(string)

	body := make([]*client.UpgradeClusterAddonsRequestBody, 0)
	b := &client.UpgradeClusterAddonsRequestBody{
		ComponentName: tea.String(d.Get("name").(string)),
	}
	if updateVersion {
		b.NextVersion = tea.String(d.Get("version").(string))
		b.Policy = tea.String("overwrite")
	}

	if updateConfig {
		if config, exist := d.GetOk("config"); exist {
			b.Config = tea.String(config.(string))
		}
	}

	body = append(body, b)

	upgradeArgs := &client.UpgradeClusterAddonsRequest{
		Body: body,
	}

	wait := incrementalWait(3*time.Second, 3*time.Second)
	err := resource.Retry(5*time.Minute, func() *resource.RetryError {
		_, err := s.client.UpgradeClusterAddons(&clusterId, upgradeArgs)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "upgradeAddon", err)
	}

	return nil
}

func (s *CsClient) uninstallAddon(d *schema.ResourceData) error {
	parts, err := ParseResourceId(d.Id(), 2)
	if err != nil {
		return WrapError(err)
	}
	clusterId := parts[0]

	body := make([]*client.UnInstallClusterAddonsRequestAddons, 0)
	b := &client.UnInstallClusterAddonsRequestAddons{
		Name: tea.String(parts[1]),
	}
	if v, ok := d.GetOk("cleanup_cloud_resources"); ok {
		b.CleanupCloudResources = tea.Bool(v.(bool))
	}
	body = append(body, b)

	uninstallArgs := &client.UnInstallClusterAddonsRequest{
		Addons: body,
	}

	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		_, err = s.client.UnInstallClusterAddons(&clusterId, uninstallArgs)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "uninstallAddon", err)
	}

	return nil
}

func (s *CsClient) updateAddonConfig(d *schema.ResourceData) error {
	clusterId := d.Get("cluster_id").(string)
	ComponentName := d.Get("name").(string)

	upgradeArgs := &client.ModifyClusterAddonRequest{
		Config: tea.String(d.Get("config").(string)),
	}

	var err error
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		_, err = s.client.ModifyClusterAddon(&clusterId, &ComponentName, upgradeArgs)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "upgradeAddonConfig", err)
	}

	return nil
}

// This function returns the status of all available addons of the cluster
func (s *CsClient) DescribeCsKubernetesAddonMetadata(clusterId string, name string, version string) (*Component, error) {
	var err error
	req := &client.DescribeClusterAddonMetadataRequest{
		Version: tea.String(version),
	}
	var resp *client.DescribeClusterAddonMetadataResponse
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		resp, err = s.client.DescribeClusterAddonMetadata(&clusterId, &name, req)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return nil, WrapErrorf(err, DefaultErrorMsg, ResourceAlicloudCSKubernetesAddon, "DescribeCsKubernetesExistedAddons", err)
	}
	result := &Component{
		ComponentName: *resp.Body.Name,
		Version:       *resp.Body.Version,
		ConfigSchema:  *resp.Body.ConfigSchema,
	}

	return result, nil
}

func (s *CsService) DescribeCsKubernetesNodePool(id string) (nodePool *cs.NodePoolDetail, err error) {
	invoker := NewInvoker()
	var requestInfo *cs.Client
	var response interface{}

	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return nil, WrapError(err)
	}
	clusterId := parts[0]
	nodePoolId := parts[1]

	if err := invoker.Run(func() error {
		raw, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			requestInfo = csClient
			return csClient.DescribeNodePoolDetail(clusterId, nodePoolId)
		})
		response = raw
		return err
	}); err != nil {
		if IsExpectedErrors(err, []string{"ErrorClusterNotFound", "400"}) {
			return nil, WrapErrorf(err, NotFoundMsg, DenverdinoAliyungo)
		}
		return nil, WrapErrorf(err, DefaultErrorMsg, nodePoolId, "DescribeNodePool", DenverdinoAliyungo)
	}
	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["ClusterId"] = clusterId
		requestMap["NodePoolId"] = nodePoolId
		addDebug("DescribeNodepool", response, requestInfo, requestMap)
	}
	nodePool, _ = response.(*cs.NodePoolDetail)
	if nodePool.NodePoolId != nodePoolId {
		return nil, WrapErrorf(NotFoundErr("CsNodePool", nodePoolId), NotFoundMsg, ProviderERROR)
	}
	return
}

func (s *CsService) WaitForCsKubernetes(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	for {
		object, err := s.DescribeCsKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.ClusterId == id && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.ClusterId, id, ProviderERROR)
		}
		time.Sleep(DefaultIntervalShort * time.Second)

	}
}

func (s *CsService) DescribeCsManagedKubernetes(id string) (cluster *cs.KubernetesClusterDetail, err error) {
	var requestInfo *cs.Client
	invoker := NewInvoker()
	var response interface{}

	if err := invoker.Run(func() error {
		raw, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			requestInfo = csClient
			return csClient.DescribeKubernetesClusterDetail(id)
		})
		response = raw
		return err
	}); err != nil {
		if IsExpectedErrors(err, []string{"ErrorClusterNotFound"}) {
			return cluster, WrapErrorf(err, NotFoundMsg, AlibabaCloudSdkGoERROR)
		}
		return cluster, WrapErrorf(err, DefaultErrorMsg, id, "DescribeKubernetesCluster", DenverdinoAliyungo)
	}
	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["Id"] = id
		addDebug("DescribeKubernetesCluster", response, requestInfo, requestMap, map[string]interface{}{"Id": id})
	}
	cluster, _ = response.(*cs.KubernetesClusterDetail)
	if cluster.ClusterId != id {
		return cluster, WrapErrorf(NotFoundErr("CSManagedKubernetes", id), NotFoundMsg, ProviderERROR)
	}
	return

}

func (s *CsService) WaitForCSManagedKubernetes(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	for {
		object, err := s.DescribeCsManagedKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.ClusterId == id && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.ClusterId, id, ProviderERROR)
		}
		time.Sleep(DefaultIntervalShort * time.Second)

	}
}

func (s *CsService) CsKubernetesInstanceStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if string(object.State) == failState {
				return object, string(object.State), WrapError(Error(FailedToReachTargetStatus, string(object.State)))
			}
		}
		return object, string(object.State), nil
	}
}

func (s *CsService) CsKubernetesNodePoolStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsKubernetesNodePool(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if string(object.State) == failState {
				return object, string(object.State), WrapError(Error(FailedToReachTargetStatus, string(object.State)))
			}
		}
		return object, string(object.State), nil
	}
}

func (s *CsService) CsManagedKubernetesInstanceStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsManagedKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if string(object.State) == failState {
				return object, string(object.State), WrapError(Error(FailedToReachTargetStatus, string(object.State)))
			}
		}
		return object, string(object.State), nil
	}
}

func (s *CsService) CsServerlessKubernetesInstanceStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeCsServerlessKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if string(object.State) == failState {
				return object, string(object.State), WrapError(Error(FailedToReachTargetStatus, string(object.State)))
			}
		}
		return object, string(object.State), nil
	}
}

func (s *CsService) DescribeCsServerlessKubernetes(id string) (*cs.ServerlessClusterResponse, error) {
	cluster := &cs.ServerlessClusterResponse{}
	var requestInfo *cs.Client
	invoker := NewInvoker()
	var response interface{}

	if err := invoker.Run(func() error {
		raw, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			requestInfo = csClient
			return csClient.DescribeServerlessKubernetesCluster(id)
		})
		response = raw
		return err
	}); err != nil {
		if IsExpectedErrors(err, []string{"ErrorClusterNotFound"}) {
			return cluster, WrapErrorf(err, NotFoundMsg, DenverdinoAliyungo)
		}
		return cluster, WrapErrorf(err, DefaultErrorMsg, id, "DescribeServerlessKubernetesCluster", DenverdinoAliyungo)
	}
	if debugOn() {
		requestMap := make(map[string]interface{})
		requestMap["Id"] = id
		addDebug("DescribeServerlessKubernetesCluster", response, requestInfo, requestMap, map[string]interface{}{"Id": id})
	}
	cluster, _ = response.(*cs.ServerlessClusterResponse)
	if cluster != nil && cluster.ClusterId != id {
		return cluster, WrapErrorf(NotFoundErr("CSServerlessKubernetes", id), NotFoundMsg, ProviderERROR)
	}
	return cluster, nil

}

func (s *CsService) WaitForCSServerlessKubernetes(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	for {
		object, err := s.DescribeCsServerlessKubernetes(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.ClusterId == id && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.ClusterId, id, ProviderERROR)
		}
		time.Sleep(DefaultIntervalShort * time.Second)

	}
}

func (s *CsService) tagsToMap(tags []cs.Tag) map[string]string {
	result := make(map[string]string)
	for _, t := range tags {
		if !s.ignoreTag(t) {
			result[t.Key] = t.Value
		}
	}
	return result
}

func (s *CsService) ignoreTag(t cs.Tag) bool {
	filter := []string{"^http://", "^https://"}
	for _, v := range filter {
		log.Printf("[DEBUG] Matching prefix %v with %v\n", v, t.Key)
		ok, _ := regexp.MatchString(v, t.Key)
		if ok {
			log.Printf("[DEBUG] Found Alibaba Cloud specific t %s (val: %s), ignoring.\n", t.Key, t.Value)
			return true
		}
	}
	return false
}

func (s *CsService) GetPermanentToken(clusterId string) (string, error) {

	describeClusterTokensResponse, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
		return csClient.DescribeClusterTokens(clusterId)
	})
	if err != nil {
		return "", WrapError(fmt.Errorf("failed to get permanent token,because of %v", err))
	}

	tokens, ok := describeClusterTokensResponse.([]*cs.ClusterTokenResponse)

	if ok != true {
		return "", WrapError(fmt.Errorf("failed to parse ClusterTokenResponse of cluster %s", clusterId))
	}

	permanentTokens := make([]string, 0)

	for _, token := range tokens {
		if token.Expired == 0 && token.IsActive == 1 {
			permanentTokens = append(permanentTokens, token.Token)
			break
		}
	}

	// create a new token
	if len(permanentTokens) == 0 {
		createClusterTokenResponse, err := s.client.WithCsClient(func(csClient *cs.Client) (interface{}, error) {
			clusterTokenReqeust := &cs.ClusterTokenReqeust{}
			clusterTokenReqeust.IsPermanently = true
			return csClient.CreateClusterToken(clusterId, clusterTokenReqeust)
		})
		if err != nil {
			return "", WrapError(fmt.Errorf("failed to create permanent token,because of %v", err))
		}

		token, ok := createClusterTokenResponse.(*cs.ClusterTokenResponse)
		if ok != true {
			return "", WrapError(fmt.Errorf("failed to parse token of %s", clusterId))
		}
		return token.Token, nil
	}

	return permanentTokens[0], nil
}

// GetUserData of cluster
func (s *CsService) GetUserData(clusterId string, labels string, taints string) (string, error) {

	token, err := s.GetPermanentToken(clusterId)

	if err != nil {
		return "", err
	}

	if labels == "" {
		labels = fmt.Sprintf("%s=true", DefaultECSTag)
	} else {
		labels = fmt.Sprintf("%s,%s=true", labels, DefaultECSTag)
	}

	cluster, err := s.DescribeCsKubernetes(clusterId)

	if err != nil {
		return "", WrapError(fmt.Errorf("failed to describe cs kuberentes cluster,because of %v", err))
	}

	extra_options := make([]string, 0)

	if len(labels) > 0 || len(taints) > 0 {

		if len(labels) != 0 {
			extra_options = append(extra_options, fmt.Sprintf("--labels %s", labels))
		}

		if len(taints) != 0 {
			extra_options = append(extra_options, fmt.Sprintf("--taints %s", taints))
		}
	}

	if network, err := GetKubernetesNetworkName(cluster); err == nil && network != "" {
		extra_options = append(extra_options, fmt.Sprintf("--network %s", network))
	}

	extra_options_in_line := strings.Join(extra_options, " ")

	version := cluster.CurrentVersion
	region := cluster.RegionId

	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(ATTACH_SCRIPT_WITH_VERSION+extra_options_in_line, region, region, version, token))), nil
}

func (s *CsClient) DescribeTaskRefreshFunc(d *schema.ResourceData, taskId string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		var err error
		var taskInfo *client.DescribeTaskInfoResponse
		wait := incrementalWait(3*time.Second, 3*time.Second)
		err = resource.Retry(5*time.Minute, func() *resource.RetryError {
			taskInfo, err = s.client.DescribeTaskInfo(tea.String(taskId))
			if err != nil {
				if NeedRetry(err) {
					wait()
					return resource.RetryableError(err)
				}
				return resource.NonRetryableError(err)
			}
			return nil
		})

		if err != nil {
			if NotFoundError(err) {
				return taskInfo, "", nil
			}
			return nil, "", WrapError(err)
		}

		currentState := tea.StringValue(taskInfo.Body.State)
		for _, failState := range failStates {
			if currentState == failState {
				if taskInfo.Body.Error != nil {
					return taskInfo.Body.Error, currentState, WrapError(Error(FailedToReachTargetStatus, currentState))
				}

				return taskInfo, currentState, WrapError(Error(FailedToReachTargetStatus, currentState))
			}
		}
		return taskInfo, currentState, nil
	}
}

func GetKubernetesNetworkName(cluster *cs.KubernetesClusterDetail) (network string, err error) {

	metadata := make(map[string]interface{})
	if err := json.Unmarshal([]byte(cluster.MetaData), &metadata); err != nil {
		return "", fmt.Errorf("unmarshal metaData failed. error: %s", err)
	}

	for _, name := range NETWORK_ADDON_NAMES {
		if _, ok := metadata[fmt.Sprintf("%s%s", name, "Version")]; ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("no network addon found")
}

func (s *CsClient) DescribeUserPermission(uid string) ([]*client.DescribeUserPermissionResponseBody, error) {
	body, err := s.client.DescribeUserPermission(tea.String(uid))
	if err != nil {
		return nil, err
	}

	return body.Body, err
}

func setCerts(d *schema.ResourceData, meta interface{}, skipSetCertificateAuthority bool) error {
	client := meta.(*connectivity.AliyunClient)
	roaClient, err := client.NewRoaCsClient()
	if err != nil {
		return WrapError(err)
	}
	csClient := CsClient{roaClient}
	kubeConfig, err := csClient.DescribeClusterKubeConfigWithExpiration(d.Id(), 0)
	if err != nil {
		log.Printf("[ERROR] Failed to get kubeconfig due to %++v", err)
	}
	m := flattenAlicloudCSCertificate(kubeConfig)
	if len(m) >= 3 {
		if ce, ok := d.GetOk("client_cert"); ok && ce.(string) != "" {
			if err := writeToFile(ce.(string), m["client_cert"]); err != nil {
				return WrapError(err)
			}
		}
		if key, ok := d.GetOk("client_key"); ok && key.(string) != "" {
			if err := writeToFile(key.(string), m["client_key"]); err != nil {
				return WrapError(err)
			}
		}
		if ca, ok := d.GetOk("cluster_ca_cert"); ok && ca.(string) != "" {
			if err := writeToFile(ca.(string), m["cluster_cert"]); err != nil {
				return WrapError(err)
			}
		}
	}
	// kube_config
	if file, ok := d.GetOk("kube_config"); ok && file.(string) != "" {
		writeToFile(file.(string), tea.StringValue(kubeConfig.Config))
	}

	if skipSetCertificateAuthority {
		d.Set("certificate_authority", map[string]string{
			"cluster_cert": "",
			"client_cert":  "",
			"client_key":   "",
		})
	} else {
		if err := d.Set("certificate_authority", flattenAlicloudCSCertificate(kubeConfig)); err != nil {
			return WrapError(fmt.Errorf("error setting certificate_authority: %s", err))
		}
	}

	return nil
}
