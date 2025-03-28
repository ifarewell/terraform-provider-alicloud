package alicloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	slsPop "github.com/aliyun/alibaba-cloud-sdk-go/services/sls"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
)

var SlsClientTimeoutCatcher = Catcher{LogClientTimeout, 15, 5}

type LogService struct {
	client *connectivity.AliyunClient
}

func (s *LogService) DescribeLogProject(id string) (*sls.LogProject, error) {
	project := &sls.LogProject{}
	var requestInfo *sls.Client
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetProject(id)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{LogClientTimeout, "ProjectForbidden"}) {
				time.Sleep(5 * time.Second)
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetProject", raw, requestInfo, map[string]string{"name": id})
		}
		project, _ = raw.(*sls.LogProject)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist"}) {
			return project, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return project, WrapErrorf(err, DefaultErrorMsg, id, "GetProject", AliyunLogGoSdkERROR)
	}
	if project == nil || project.Name == "" {
		return project, WrapErrorf(NotFoundErr("LogProject", id), NotFoundMsg, ProviderERROR)
	}
	return project, nil
}

func (s *LogService) WaitForLogProject(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		object, err := s.DescribeLogProject(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == id && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, id, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogStore(id string) (*sls.LogStore, error) {
	store := &sls.LogStore{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return store, WrapError(err)
	}
	projectName, name := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetLogStore(projectName, name)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogStore", raw, requestInfo, map[string]string{
				"project":  projectName,
				"logstore": name,
			})
		}
		store, _ = raw.(*sls.LogStore)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "LogStoreNotExist"}) {
			return store, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return store, WrapErrorf(err, DefaultErrorMsg, id, "GetLogStore", AliyunLogGoSdkERROR)
	}
	if store == nil || store.Name == "" {
		return store, WrapErrorf(NotFoundErr("LogStore", id), NotFoundMsg, ProviderERROR)
	}
	return store, nil
}

func (s *LogService) WaitForLogStore(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	name := parts[1]
	for {
		object, err := s.DescribeLogStore(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == name && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, name, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogStoreIndex(id string) (*sls.Index, error) {
	index := &sls.Index{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return index, WrapError(err)
	}
	projectName, name := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetIndex(projectName, name)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetIndex", raw, requestInfo, map[string]string{
				"project":  projectName,
				"logstore": name,
			})
		}
		index, _ = raw.(*sls.Index)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "LogStoreNotExist", "IndexConfigNotExist"}) {
			return index, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return index, WrapErrorf(err, DefaultErrorMsg, id, "GetIndex", AliyunLogGoSdkERROR)
	}

	if index == nil || (index.Line == nil && index.Keys == nil) {
		return index, WrapErrorf(NotFoundErr("LogStoreIndex", id), NotFoundMsg, ProviderERROR)
	}
	return index, nil
}

func (s *LogService) DescribeLogMachineGroup(id string) (*sls.MachineGroup, error) {
	group := &sls.MachineGroup{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return group, WrapError(err)
	}
	projectName, groupName := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetMachineGroup(projectName, groupName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetMachineGroup", raw, requestInfo, map[string]string{
				"project":      projectName,
				"machineGroup": groupName,
			})
		}
		group, _ = raw.(*sls.MachineGroup)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "GroupNotExist", "MachineGroupNotExist"}) {
			return group, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return group, WrapErrorf(err, DefaultErrorMsg, id, "GetMachineGroup", AliyunLogGoSdkERROR)
	}

	if group == nil || group.Name == "" {
		return group, WrapErrorf(NotFoundErr("LogMachineGroup", id), NotFoundMsg, ProviderERROR)
	}
	return group, nil
}

func (s *LogService) WaitForLogMachineGroup(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	name := parts[1]
	for {
		object, err := s.DescribeLogMachineGroup(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == name && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, name, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogtailConfig(id string) (*sls.LogConfig, error) {
	response := &sls.LogConfig{}
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return response, WrapError(err)
	}
	projectName, configName := parts[0], parts[2]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			slsClient.RetryTimeOut = 30 * time.Second
			return slsClient.GetConfig(projectName, configName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"unconvertable", "Unconvertable"}) {
				return resource.NonRetryableError(err)
			}
			if IsExpectedErrors(err, []string{"InternalServerError"}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetConfig", raw, requestInfo, map[string]string{
				"project": projectName,
				"config":  configName,
			})
		}
		response, _ = raw.(*sls.LogConfig)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "LogStoreNotExist", "ConfigNotExist"}) {
			return response, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return response, WrapErrorf(err, DefaultErrorMsg, id, "GetConfig", AliyunLogGoSdkERROR)
	}
	if response == nil || response.Name == "" {
		return response, WrapErrorf(NotFoundErr("LogTailConfig", id), NotFoundMsg, ProviderERROR)
	}
	return response, nil
}

func (s *LogService) WaitForLogtailConfig(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return WrapError(err)
	}
	name := parts[2]
	for {
		object, err := s.DescribeLogtailConfig(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == name && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, name, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogtailAttachment(id string) (groupName string, err error) {
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return groupName, WrapError(err)
	}
	projectName, configName, name := parts[0], parts[1], parts[2]
	var groupNames []string
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {

		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetAppliedMachineGroups(projectName, configName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError"}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetAppliedMachineGroups", raw, requestInfo, map[string]string{
				"project":  projectName,
				"confName": configName,
			})
		}
		groupNames, _ = raw.([]string)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "ConfigNotExist", "MachineGroupNotExist"}) {
			return groupName, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return groupName, WrapErrorf(err, DefaultErrorMsg, id, "GetAppliedMachineGroups", AliyunLogGoSdkERROR)
	}
	for _, group_name := range groupNames {
		if group_name == name {
			groupName = group_name
		}
	}
	if groupName == "" {
		return groupName, WrapErrorf(NotFoundErr("LogtailAttachment", id), NotFoundMsg, ProviderERROR)
	}
	return groupName, nil
}

func (s *LogService) WaitForLogtailAttachment(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return WrapError(err)
	}
	name := parts[2]
	for {
		object, err := s.DescribeLogtailAttachment(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object == name && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object, name, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogAlertResource(id string) (map[string]string, error) {
	var result = map[string]string{}
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return result, WrapError(err)
	}
	resourceType := parts[1]
	if err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		_, err := s.client.WithLogPopClient(func(slsPopClient *slsPop.Client) (interface{}, error) {
			switch resourceType {
			case "user":
				_, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
					record, err := slsClient.GetResourceRecord("sls.alert.global_config", "default_config")
					if err != nil {
						return nil, err
					}
					var alertGlobalConfig AlertGlobalConfig
					err = json.Unmarshal([]byte(record.Value), &alertGlobalConfig)
					if err != nil {
						return nil, err
					}
					region := alertGlobalConfig.ConfigDetail.AlertCenterLog.Region
					accountId, err := s.client.AccountId()
					if err != nil {
						return nil, err
					}
					projectName := fmt.Sprintf("sls-alert-%s-%s", accountId, region)
					endpoint := slsClient.Endpoint
					slsClient.Endpoint = strings.Replace(endpoint, s.client.RegionId, region, 1)
					_, err = slsClient.GetProject(projectName)
					if err != nil {
						slsClient.Endpoint = endpoint
						return nil, err
					}
					_, err = slsClient.GetLogStore(projectName, "internal-alert-center-log")
					slsClient.Endpoint = endpoint
					if err != nil {
						return nil, err
					}
					return nil, nil
				})
				if err != nil {
					if IsExpectedErrors(err, []string{"ProjectNotExist"}) || IsExpectedErrors(err, []string{"LogStoreNotExist"}) {
						return result, nil
					}
					return nil, err
				}
				lang := parts[2]
				result["type"] = resourceType
				result["project"] = ""
				result["lang"] = lang
				return result, nil
			case "project":
				project := parts[2]
				_, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
					return slsClient.GetLogStore(project, "internal-alert-history")
				})
				if err != nil {
					if IsExpectedErrors(err, []string{"LogStoreNotExist"}) {
						return nil, nil
					}
					return nil, err
				}
				result["type"] = resourceType
				result["project"] = project
				result["lang"] = ""
				return result, nil
			default:
				return result, WrapErrorf(errors.New("type error"), DefaultErrorMsg, "alicloud_log_alert_resource", "ReadAlertResource", AliyunLogGoSdkERROR)
			}
		})
		if err != nil {
			if IsExpectedErrors(err, []string{LogClientTimeout}) {
				time.Sleep(5 * time.Second)
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	}); err != nil {
		return result, WrapErrorf(err, DefaultErrorMsg, "alicloud_log_alert_resource", "ReadAlertResource", AliyunLogGoSdkERROR)
	}
	return result, nil
}

func (s *LogService) DescribeLogAlert(id string) (*sls.Alert, error) {
	alert := &sls.Alert{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return alert, WrapError(err)
	}
	projectName, alertName := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetAlert(projectName, alertName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogstoreAlert", raw, requestInfo, map[string]string{
				"project":    projectName,
				"alert_name": alertName,
			})
		}
		alert, _ = raw.(*sls.Alert)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "JobNotExist"}) {
			return alert, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return alert, WrapErrorf(err, DefaultErrorMsg, id, "GetLogstoreAlert", AliyunLogGoSdkERROR)
	}

	if alert == nil || alert.Name == "" {
		return alert, WrapErrorf(NotFoundErr("LogstoreAlert", id), NotFoundMsg, ProviderERROR)
	}
	return alert, nil
}

func (s *LogService) WaitForLogstoreAlert(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	name := parts[1]
	for {
		object, err := s.DescribeLogAlert(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == name && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, name, ProviderERROR)
		}
	}
}

func (s *LogService) CreateLogDashboard(project, name string) error {
	dashboard := sls.Dashboard{
		DashboardName: name,
		ChartList:     []sls.Chart{},
	}
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			return nil, slsClient.CreateDashboard(project, dashboard)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			if err.(*sls.Error).Message == "specified dashboard already exists" {
				return nil
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("CreateLogDashboard", raw, map[string]string{
				"project":        project,
				"dashboard_name": name,
			})
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func CreateDashboard(project, name string, client *sls.Client) error {
	dashboard := sls.Dashboard{
		DashboardName: name,
		ChartList:     []sls.Chart{},
	}
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		err := client.CreateDashboard(project, dashboard)
		if err != nil {
			if err.(*sls.Error).Message == "specified dashboard already exists" {
				return nil
			}
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}

		}
		if debugOn() {
			addDebug("CreateLogDashboard", dashboard, map[string]string{
				"project":        project,
				"dashboard_name": name,
			})
		}
		return nil
	})
	return err
}

func (s *LogService) DescribeLogAudit(id string) (*slsPop.DescribeAppResponse, error) {
	request := slsPop.CreateDescribeAppRequest()
	response := &slsPop.DescribeAppResponse{}
	request.AppName = "audit"
	raw, err := s.client.WithLogPopClient(func(client *slsPop.Client) (interface{}, error) {
		return client.DescribeApp(request)
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"AppNotExist"}) {
			return response, WrapErrorf(err, NotFoundMsg, AlibabaCloudSdkGoERROR)
		}
	}
	addDebug(request.GetActionName(), raw, request.RpcRequest, request)
	response, _ = raw.(*slsPop.DescribeAppResponse)
	return response, nil
}

func GetCharTitile(project, dashboard, char string, client *sls.Client) string {
	board, err := client.GetDashboard(project, dashboard)
	// If the query fails to ignore the error, return the original value.
	if err != nil {
		return char
	}
	for _, v := range board.ChartList {
		if v.Display.DisplayName == char {
			return v.Title
		} else {
			return char
		}

	}
	return char
}

func (s *LogService) DescribeLogDashboard(id string) (string, error) {
	dashboard := ""
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return dashboard, WrapError(err)
	}
	projectName, dashboardName := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetDashboardString(projectName, dashboardName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogstoreDashboard", raw, requestInfo, map[string]string{
				"project":        projectName,
				"dashboard_name": dashboardName,
			})
		}
		dashboard, _ = raw.(string)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "DashboardNotExist"}) {
			return dashboard, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return dashboard, WrapErrorf(err, DefaultErrorMsg, id, "GetLogstoreDashboard", AliyunLogGoSdkERROR)
	}

	if dashboard == "" {
		return dashboard, WrapErrorf(NotFoundErr("LogstoreDashboard", id), NotFoundMsg, ProviderERROR)
	}
	return dashboard, nil
}

func (s *LogService) WaitForLogDashboard(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	name := parts[1]
	for {
		objectString, err := s.DescribeLogDashboard(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if objectString != "" && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, objectString, name, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogProjectPolicy(id string) (string, error) {
	policy := ""
	projectName := id
	var requestInfo *sls.Client
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetProjectPolicy(projectName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogProjectPolicy", raw, requestInfo, map[string]string{
				"project": projectName,
			})
		}
		policy, _ = raw.(string)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist"}) {
			return policy, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return policy, WrapErrorf(err, DefaultErrorMsg, id, "GetProjectPolicy", AliyunLogGoSdkERROR)
	}

	return policy, nil
}

func (s *LogService) DescribeLogProjectTags(project_name string) ([]*sls.ResourceTagResponse, error) {
	var requestInfo *sls.Client
	var respTags []*sls.ResourceTagResponse

	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			raw, _, err := slsClient.ListTagResources(project_name, "project", []string{project_name}, []sls.ResourceFilterTag{}, "")
			return raw, err
		})

		if err != nil {
			if IsExpectedErrors(err, []string{LogClientTimeout}) {
				time.Sleep(5 * time.Second)
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetProjectTags", raw, requestInfo, map[string]string{"project_name": project_name})
		}
		respTags = raw.([]*sls.ResourceTagResponse)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist"}) {
			return respTags, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return respTags, WrapErrorf(err, DefaultErrorMsg, project_name, "GetProejctTags", AliyunLogGoSdkERROR)
	}
	return respTags, nil
}

func (s *LogService) DescribeLogEtl(id string) (*sls.ETL, error) {
	etl := &sls.ETL{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return etl, WrapError(err)
	}
	projectName, etlName := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetETL(projectName, etlName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogETL", raw, requestInfo, map[string]string{
				"project":  projectName,
				"etl_name": etlName,
			})
		}
		etl, _ = raw.(*sls.ETL)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "JobNotExist"}) {
			return etl, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return etl, WrapErrorf(err, DefaultErrorMsg, id, "GetETL", AliyunLogGoSdkERROR)
	}
	return etl, nil
}

func (s *LogService) WaitForLogETL(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	for {
		object, err := s.DescribeLogEtl(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == parts[1] && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, id, ProviderERROR)
		}
	}
}

func (s *LogService) LogETLStateRefreshFunc(id string, failStates []string, slsClient *sls.Client) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		parts, err := ParseResourceId(id, 2)
		if err != nil {
			return nil, "", WrapError(err)
		}
		object, err := slsClient.GetETL(parts[0], parts[1])
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if object.Status == failState {
				return object, object.Status, WrapError(Error(FailedToReachTargetStatus, object.Status))
			}
		}

		return object, object.Status, nil
	}
}

func (s *LogService) LogOssShipperStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeLogEtl(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if object.Status == failState {
				return object, object.Status, WrapError(Error(FailedToReachTargetStatus, object.Status))
			}
		}

		return object, object.Status, nil
	}
}

func (s *LogService) DescribeLogOssShipper(id string) (*sls.Shipper, error) {
	var shipper *sls.Shipper
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return shipper, WrapError(err)
	}
	projectName, logstoreName, shipperName := parts[0], parts[1], parts[2]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			project, _ := sls.NewLogProject(projectName, slsClient.Endpoint, slsClient.AccessKeyID, slsClient.AccessKeySecret)
			project, _ = project.WithToken(slsClient.SecurityToken)
			logstore, _ := sls.NewLogStore(logstoreName, project)
			return logstore.GetShipper(shipperName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetLogOssShipper", raw, requestInfo, map[string]string{
				"project_name":  projectName,
				"logstore_name": logstoreName,
				"shipper_name":  shipperName,
			})
		}
		shipper, _ = raw.(*sls.Shipper)
		return nil
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist"}) {
			return shipper, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		// SLS server problem, temporarily by returning nil value to solve.
		if d, ok := err.(*sls.Error); ok {
			if d.Message == fmt.Sprintf("shipperName %s does not exist", parts[2]) {
				return shipper, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
			}

		}
		return shipper, WrapErrorf(err, DefaultErrorMsg, id, "GetLogOssShipper", AliyunLogGoSdkERROR)
	}
	return shipper, nil
}

func (s *LogService) WaitForLogOssShipper(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return WrapError(err)
	}
	for {
		object, err := s.DescribeLogOssShipper(id)
		if err != nil {
			if object == nil {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.ShipperName == parts[2] && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.ShipperName, id, ProviderERROR)
		}
	}
}

func (s *LogService) LogProjectStateRefreshFunc(id string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		object, err := s.DescribeLogProject(id)
		if err != nil {
			if NotFoundError(err) {
				// Set this to nil as if we didn't find anything.
				return nil, "", nil
			}
			return nil, "", WrapError(err)
		}

		for _, failState := range failStates {
			if object.Status == failState {
				return object, object.Status, WrapError(Error(FailedToReachTargetStatus, object.Status))
			}
		}
		return object, object.Status, nil
	}
}

func (s *LogService) DescribeLogIngestion(id string) (*sls.Ingestion, error) {
	var ingestion *sls.Ingestion
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return ingestion, WrapError(err)
	}
	projectName, logstoreName, ingestionName := parts[0], parts[1], parts[2]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetIngestion(projectName, ingestionName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetIngestion", raw, requestInfo, map[string]string{
				"project":        projectName,
				"logstore":       logstoreName,
				"ingestion_name": ingestionName,
			})
		}
		ingestion, _ = raw.(*sls.Ingestion)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "JobNotExist"}) {
			return ingestion, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return ingestion, WrapErrorf(err, DefaultErrorMsg, id, "GetIngestion", AliyunLogGoSdkERROR)
	}
	return ingestion, nil
}

func (s *LogService) WaitForLogIngestion(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return WrapError(err)
	}
	for {
		object, err := s.DescribeLogIngestion(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == parts[2] && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, id, ProviderERROR)
		}
	}
}
func (s *LogService) DescribeLogOssExport(id string) (*sls.Export, error) {
	return s.describeLogExport(id)
}

func (s *LogService) describeLogExport(id string) (*sls.Export, error) {
	var export *sls.Export
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return export, WrapError(err)
	}
	projectName, logstoreName, exportName := parts[0], parts[1], parts[2]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetExport(projectName, exportName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetExport", raw, requestInfo, map[string]string{
				"project":     projectName,
				"logstore":    logstoreName,
				"export_name": exportName,
			})
		}
		export, _ = raw.(*sls.Export)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ProjectNotExist", "JobNotExist"}) {
			return export, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return export, WrapErrorf(err, DefaultErrorMsg, id, "GetExport", AliyunLogGoSdkERROR)
	}
	return export, nil
}

func (s *LogService) WaitForLogOssExport(id string, status Status, timeout int) error {
	return s.waitForLogExport(id, status, timeout)
}

func (s *LogService) waitForLogExport(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	parts, err := ParseResourceId(id, 3)
	if err != nil {
		return WrapError(err)
	}
	for {
		object, err := s.DescribeLogOssExport(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == parts[2] && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, id, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogResource(id string) (*sls.Resource, error) {
	res := &sls.Resource{}
	resourceName := id
	var requestInfo *sls.Client
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetResource(resourceName)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetResource", raw, requestInfo, map[string]string{
				"resource_name": resourceName,
			})
		}
		res, _ = raw.(*sls.Resource)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ResourceNotExist"}) {
			return res, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return res, WrapErrorf(err, DefaultErrorMsg, id, "GetLogResource", AliyunLogGoSdkERROR)
	}

	if res == nil {
		return res, WrapErrorf(NotFoundErr("LogResource", id), NotFoundMsg, ProviderERROR)
	}
	return res, nil
}

func (s *LogService) WaitForLogResource(id string, status Status, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	resourceName := id
	for {
		object, err := s.DescribeLogResource(resourceName)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Name == resourceName && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Name, resourceName, ProviderERROR)
		}
	}
}

func (s *LogService) DescribeLogResourceRecord(id string) (*sls.ResourceRecord, error) {
	res := &sls.ResourceRecord{}
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return res, WrapError(err)
	}
	resourceName, recordId := parts[0], parts[1]
	var requestInfo *sls.Client
	err = resource.Retry(2*time.Minute, func() *resource.RetryError {
		raw, err := s.client.WithLogClient(func(slsClient *sls.Client) (interface{}, error) {
			requestInfo = slsClient
			return slsClient.GetResourceRecord(resourceName, recordId)
		})
		if err != nil {
			if IsExpectedErrors(err, []string{"InternalServerError", LogClientTimeout}) {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		if debugOn() {
			addDebug("GetResourceRecord", raw, requestInfo, map[string]string{
				"resource_name": resourceName,
				"record_id":     recordId,
			})
		}
		res, _ = raw.(*sls.ResourceRecord)
		return nil
	})

	if err != nil {
		if IsExpectedErrors(err, []string{"ResourceNotExist", "ResourceRecordNotExist"}) {
			return res, WrapErrorf(err, NotFoundMsg, AliyunLogGoSdkERROR)
		}
		return res, WrapErrorf(err, DefaultErrorMsg, id, "GetLogResourceRecord", AliyunLogGoSdkERROR)
	}

	if res == nil {
		return res, WrapErrorf(NotFoundErr("LogResourceRecord", id), NotFoundMsg, ProviderERROR)
	}
	return res, nil
}

func (s *LogService) WaitForLogResourceRecord(id string, status Status, timeout int) error {
	parts, err := ParseResourceId(id, 2)
	if err != nil {
		return WrapError(err)
	}
	recordId := parts[1]
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		object, err := s.DescribeLogResourceRecord(id)
		if err != nil {
			if NotFoundError(err) {
				if status == Deleted {
					return nil
				}
			} else {
				return WrapError(err)
			}
		}
		if object.Id == recordId && status != Deleted {
			return nil
		}
		if time.Now().After(deadline) {
			return WrapErrorf(err, WaitTimeoutMsg, id, GetFunc(1), timeout, object.Id, recordId, ProviderERROR)
		}
	}
}
