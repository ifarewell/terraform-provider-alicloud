package alicloud

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/stretchr/testify/assert"

	"github.com/PaesslerAG/jsonpath"
	util "github.com/alibabacloud-go/tea-utils/service"

	"github.com/alibabacloud-go/tea-rpc/client"
	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
)

func init() {
	resource.AddTestSweepers("alicloud_resource_manager_resource_group", &resource.Sweeper{
		Name: "alicloud_resource_manager_resource_group",
		F:    testSweepResourceManagerResourceGroup,
	})
}

func testSweepResourceManagerResourceGroup(region string) error {
	rawClient, err := sharedClientForRegion(region)
	if err != nil {
		return WrapErrorf(err, "Error getting AliCloud client.")
	}
	client := rawClient.(*connectivity.AliyunClient)
	prefixes := []string{
		"tf-",
		"tf-rd",
		"tf-testacc",
	}

	action := "ListResourceGroups"
	request := make(map[string]interface{})
	var response map[string]interface{}
	var groupIds []string
	request["PageSize"] = PageSizeLarge
	request["PageNumber"] = 1
	defaultGroupId := ""
	for {
		response, err = client.RpcPost("ResourceManager", "2020-03-31", action, nil, request, true)
		if err != nil {
			log.Printf("[ERROR] Failed to retrieve resoure manager group in service list: %s", err)
			return nil
		}
		resp, err := jsonpath.Get("$.ResourceGroups.ResourceGroup", response)
		if err != nil {
			return WrapErrorf(err, FailedGetAttributeMsg, action, "$.ResourceGroups.ResourceGroup", response)
		}
		result, _ := resp.([]interface{})
		for _, v := range result {
			item := v.(map[string]interface{})
			if item["Name"].(string) == "default" {
				defaultGroupId = item["Id"].(string)
				continue
			}
			// Skip Invalid resource group.
			if item["Status"].(string) != "OK" {
				continue
			}
			skip := true
			if !sweepAll() {
				for _, prefix := range prefixes {
					if strings.HasPrefix(strings.ToLower(item["Name"].(string)), strings.ToLower(prefix)) {
						skip = false
					}
				}
				if skip {
					log.Printf("[INFO] Skipping resource manager group: %s ", item["Name"])
					continue
				}
			}
			groupIds = append(groupIds, item["Id"].(string))
		}

		if len(result) < PageSizeLarge {
			break
		}
		request["PageNumber"] = request["PageNumber"].(int) + 1

	}

	for _, groupId := range groupIds {
		request = map[string]interface{}{
			"ResourceGroupId": groupId,
			"PageSize":        100,
		}
		action = "ListResources"
		wait := incrementalWait(3*time.Second, 3*time.Second)
		err = resource.Retry(1*time.Minute, func() *resource.RetryError {
			response, err = client.RpcPost("ResourceManager", "2020-03-31", action, nil, request, false)
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
			log.Printf("[ERROR] Failed to list resources for manager group (%s): %s", groupId, err)
		}
		if v, ok := response["Resources"]; ok {
			log.Printf("[INFO] Move resource for resource manager group: %s ", groupId)
			resources := make([]interface{}, 0)
			for i, item := range v.(map[string]interface{})["Resource"].([]interface{}) {
				r := item.(map[string]interface{})
				delete(r, "CreateDate")
				delete(r, "ResourceGroupId")
				if len(resources) >= 10 {
					resources = []interface{}{r}
				} else {
					resources = append(resources, r)
				}
				if len(resources) != 10 && i != len(v.(map[string]interface{})["Resource"].([]interface{}))-1 {
					continue
				}
				request = map[string]interface{}{
					"ResourceGroupId": defaultGroupId,
					"Resources":       resources,
				}
				action = "MoveResources"
				wait := incrementalWait(3*time.Second, 3*time.Second)
				err = resource.Retry(1*time.Minute, func() *resource.RetryError {
					response, err = client.RpcPost("ResourceManager", "2020-03-31", action, nil, request, false)
					log.Printf("[INFO] moving resource group %s %d resources got an error: %v", groupId, len(resources), err)
					if err != nil {
						if NeedRetry(err) {
							wait()
							return resource.RetryableError(err)
						}
						return resource.NonRetryableError(err)
					}
					return nil
				})
			}
		}
		log.Printf("[INFO] Delete resource manager group: %s ", groupId)
		action = "DeleteResourceGroup"
		request = map[string]interface{}{
			"ResourceGroupId": groupId,
		}
		wait = incrementalWait(3*time.Second, 3*time.Second)
		err = resource.Retry(1*time.Minute, func() *resource.RetryError {
			response, err = client.RpcPost("ResourceManager", "2020-03-31", action, nil, request, false)
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
			log.Printf("[ERROR] Failed to delete resource manager group (%s): %s", groupId, err)
		}
	}
	return nil
}

func TestAccAliCloudResourceManagerResourceGroup_basic0(t *testing.T) {
	var v map[string]interface{}
	resourceId := "alicloud_resource_manager_resource_group.default"
	ra := resourceAttrInit(resourceId, AliCloudResourceManagerResourceGroupMap0)
	rc := resourceCheckInitWithDescribeMethod(resourceId, &v, func() interface{} {
		return &ResourcemanagerService{testAccProvider.Meta().(*connectivity.AliyunClient)}
	}, "DescribeResourceManagerResourceGroup")
	rac := resourceAttrCheckInit(rc, ra)
	testAccCheck := rac.resourceAttrMapUpdateSet()
	rand := acctest.RandIntRange(1000000, 9999999)
	name := fmt.Sprintf("tf-testacc-rg%d", rand)
	testAccConfig := resourceTestAccConfigFunc(resourceId, name, AliCloudResourceManagerResourceGroupBasicDependence0)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		IDRefreshName: resourceId,
		Providers:     testAccProviders,
		CheckDestroy:  rac.checkResourceDestroy(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name":        name,
					"resource_group_name": name,
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name":        name,
						"resource_group_name": name,
					}),
				),
			},
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name": name + "update",
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name": name + "update",
					}),
				),
			},
			{
				Config: testAccConfig(map[string]interface{}{
					"tags": map[string]string{
						"Created": "TF",
						"For":     "ResourceGroup",
					},
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"tags.%":       "2",
						"tags.Created": "TF",
						"tags.For":     "ResourceGroup",
					}),
				),
			},
			{
				ResourceName:      resourceId,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAliCloudResourceManagerResourceGroup_basic0_twin(t *testing.T) {
	var v map[string]interface{}
	resourceId := "alicloud_resource_manager_resource_group.default"
	ra := resourceAttrInit(resourceId, AliCloudResourceManagerResourceGroupMap0)
	rc := resourceCheckInitWithDescribeMethod(resourceId, &v, func() interface{} {
		return &ResourcemanagerService{testAccProvider.Meta().(*connectivity.AliyunClient)}
	}, "DescribeResourceManagerResourceGroup")
	rac := resourceAttrCheckInit(rc, ra)
	testAccCheck := rac.resourceAttrMapUpdateSet()
	rand := acctest.RandIntRange(1000000, 9999999)
	name := fmt.Sprintf("tf-testacc-rg%d", rand)
	testAccConfig := resourceTestAccConfigFunc(resourceId, name, AliCloudResourceManagerResourceGroupBasicDependence0)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		IDRefreshName: resourceId,
		Providers:     testAccProviders,
		CheckDestroy:  rac.checkResourceDestroy(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name":        name,
					"resource_group_name": name,
					"tags": map[string]string{
						"Created": "TF",
						"For":     "ResourceGroup",
					},
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name":        name,
						"resource_group_name": name,
						"tags.%":              "2",
						"tags.Created":        "TF",
						"tags.For":            "ResourceGroup",
					}),
				),
			},
			{
				ResourceName:      resourceId,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAliCloudResourceManagerResourceGroup_basic1(t *testing.T) {
	var v map[string]interface{}
	resourceId := "alicloud_resource_manager_resource_group.default"
	ra := resourceAttrInit(resourceId, AliCloudResourceManagerResourceGroupMap0)
	rc := resourceCheckInitWithDescribeMethod(resourceId, &v, func() interface{} {
		return &ResourcemanagerService{testAccProvider.Meta().(*connectivity.AliyunClient)}
	}, "DescribeResourceManagerResourceGroup")
	rac := resourceAttrCheckInit(rc, ra)
	testAccCheck := rac.resourceAttrMapUpdateSet()
	rand := acctest.RandIntRange(1000000, 9999999)
	name := fmt.Sprintf("tf-testacc-rg%d", rand)
	testAccConfig := resourceTestAccConfigFunc(resourceId, name, AliCloudResourceManagerResourceGroupBasicDependence0)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		IDRefreshName: resourceId,
		Providers:     testAccProviders,
		CheckDestroy:  rac.checkResourceDestroy(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name": name,
					"name":         name,
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name": name,
						"name":         name,
					}),
				),
			},
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name": name + "update",
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name": name + "update",
					}),
				),
			},
			{
				Config: testAccConfig(map[string]interface{}{
					"tags": map[string]string{
						"Created": "TF",
						"For":     "ResourceGroup",
					},
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"tags.%":       "2",
						"tags.Created": "TF",
						"tags.For":     "ResourceGroup",
					}),
				),
			},
			{
				ResourceName:      resourceId,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAliCloudResourceManagerResourceGroup_basic1_twin(t *testing.T) {
	var v map[string]interface{}
	resourceId := "alicloud_resource_manager_resource_group.default"
	ra := resourceAttrInit(resourceId, AliCloudResourceManagerResourceGroupMap0)
	rc := resourceCheckInitWithDescribeMethod(resourceId, &v, func() interface{} {
		return &ResourcemanagerService{testAccProvider.Meta().(*connectivity.AliyunClient)}
	}, "DescribeResourceManagerResourceGroup")
	rac := resourceAttrCheckInit(rc, ra)
	testAccCheck := rac.resourceAttrMapUpdateSet()
	rand := acctest.RandIntRange(1000000, 9999999)
	name := fmt.Sprintf("tf-testacc-rg%d", rand)
	testAccConfig := resourceTestAccConfigFunc(resourceId, name, AliCloudResourceManagerResourceGroupBasicDependence0)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		IDRefreshName: resourceId,
		Providers:     testAccProviders,
		CheckDestroy:  rac.checkResourceDestroy(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfig(map[string]interface{}{
					"display_name": name,
					"name":         name,
					"tags": map[string]string{
						"Created": "TF",
						"For":     "ResourceGroup",
						"Key1":    "Value1",
						"Key2":    "Value2",
						"Key3":    "Value3",
						"Key4":    "Value4",
						"Key5":    "Value5",
						"Key6":    "Value6",
						"Key7":    "Value7",
						"Key8":    "Value8",
						"Key9":    "Value9",
						"Key10":   "Value10",
					},
				}),
				Check: resource.ComposeTestCheckFunc(
					testAccCheck(map[string]string{
						"display_name": name,
						"name":         name,
						"tags.%":       "12",
						"tags.Created": "TF",
						"tags.For":     "ResourceGroup",
						"tags.Key1":    "Value1",
						"tags.Key2":    "Value2",
						"tags.Key3":    "Value3",
						"tags.Key4":    "Value4",
						"tags.Key5":    "Value5",
						"tags.Key6":    "Value6",
						"tags.Key7":    "Value7",
						"tags.Key8":    "Value8",
						"tags.Key9":    "Value9",
						"tags.Key10":   "Value10",
					}),
				),
			},
			{
				ResourceName:      resourceId,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

var AliCloudResourceManagerResourceGroupMap0 = map[string]string{
	"resource_group_name": CHECKSET,
	"name":                CHECKSET,
	"account_id":          CHECKSET,
	"status":              CHECKSET,
	"region_statuses.#":   CHECKSET,
}

func AliCloudResourceManagerResourceGroupBasicDependence0(name string) string {
	return ""
}

func TestUnitAliCloudResourceManagerResourceGroup(t *testing.T) {
	p := Provider().(*schema.Provider).ResourcesMap
	dInit, _ := schema.InternalMap(p["alicloud_resource_manager_resource_group"].Schema).Data(nil, nil)
	dExisted, _ := schema.InternalMap(p["alicloud_resource_manager_resource_group"].Schema).Data(nil, nil)
	dInit.MarkNewResource()
	attributes := map[string]interface{}{
		"display_name":        "CreateResourceGroupValue",
		"resource_group_name": "CreateResourceGroupValue",
	}
	for key, value := range attributes {
		err := dInit.Set(key, value)
		assert.Nil(t, err)
		err = dExisted.Set(key, value)
		assert.Nil(t, err)
		if err != nil {
			log.Printf("[ERROR] the field %s setting error", key)
		}
	}
	region := os.Getenv("ALICLOUD_REGION")
	rawClient, err := sharedClientForRegion(region)
	if err != nil {
		t.Skipf("Skipping the test case with err: %s", err)
		t.Skipped()
	}
	rawClient = rawClient.(*connectivity.AliyunClient)
	ReadMockResponse := map[string]interface{}{
		// GetResourceGroup
		"ResourceGroup": map[string]interface{}{
			"Id":          "CreateResourceGroupValue",
			"AccountId":   "CreateResourceGroupValue",
			"DisplayName": "CreateResourceGroupValue",
			"RegionStatuses": map[string]interface{}{
				"RegionStatus": []interface{}{
					map[string]interface{}{
						"RegionId": "CreateResourceGroupValue",
						"Status":   "CreateResourceGroupValue",
					},
				},
			},
			"Name":   "CreateResourceGroupValue",
			"Status": "OK",
		},
	}
	CreateMockResponse := map[string]interface{}{
		// CreateResourceGroup
		"ResourceGroup": map[string]interface{}{
			"Id": "CreateResourceGroupValue",
		},
	}
	failedResponseMock := func(errorCode string) (map[string]interface{}, error) {
		return nil, &tea.SDKError{
			Code:       String(errorCode),
			Data:       String(errorCode),
			Message:    String(errorCode),
			StatusCode: tea.Int(400),
		}
	}
	notFoundResponseMock := func(errorCode string) (map[string]interface{}, error) {
		return nil, GetNotFoundErrorFromString(GetNotFoundMessage("alicloud_resource_manager_resource_group", errorCode))
	}
	successResponseMock := func(operationMockResponse map[string]interface{}) (map[string]interface{}, error) {
		if len(operationMockResponse) > 0 {
			mapMerge(ReadMockResponse, operationMockResponse)
		}
		return ReadMockResponse, nil
	}

	// Create
	patches := gomonkey.ApplyMethod(reflect.TypeOf(&connectivity.AliyunClient{}), "NewResourcemanagerClient", func(_ *connectivity.AliyunClient) (*client.Client, error) {
		return nil, &tea.SDKError{
			Code:       String("loadEndpoint error"),
			Data:       String("loadEndpoint error"),
			Message:    String("loadEndpoint error"),
			StatusCode: tea.Int(400),
		}
	})
	err = resourceAliCloudResourceManagerResourceGroupCreate(dInit, rawClient)
	patches.Reset()
	assert.NotNil(t, err)
	ReadMockResponseDiff := map[string]interface{}{
		// GetResourceGroup Response
		"ResourceGroup": map[string]interface{}{
			"Id": "CreateResourceGroupValue",
		},
	}
	errorCodes := []string{"NonRetryableError", "Throttling", "nil"}
	for index, errorCode := range errorCodes {
		retryIndex := index - 1 // a counter used to cover retry scenario; the same below
		patches := gomonkey.ApplyMethod(reflect.TypeOf(&client.Client{}), "DoRequest", func(_ *client.Client, action *string, _ *string, _ *string, _ *string, _ *string, _ map[string]interface{}, _ map[string]interface{}, _ *util.RuntimeOptions) (map[string]interface{}, error) {
			if *action == "CreateResourceGroup" {
				switch errorCode {
				case "NonRetryableError":
					return failedResponseMock(errorCode)
				default:
					retryIndex++
					if retryIndex >= len(errorCodes)-1 {
						successResponseMock(ReadMockResponseDiff)
						return CreateMockResponse, nil
					}
					return failedResponseMock(errorCodes[retryIndex])
				}
			}
			return ReadMockResponse, nil
		})
		err := resourceAliCloudResourceManagerResourceGroupCreate(dInit, rawClient)
		patches.Reset()
		switch errorCode {
		case "NonRetryableError":
			assert.NotNil(t, err)
		default:
			assert.Nil(t, err)
			dCompare, _ := schema.InternalMap(p["alicloud_resource_manager_resource_group"].Schema).Data(dInit.State(), nil)
			for key, value := range attributes {
				dCompare.Set(key, value)
			}
			assert.Equal(t, dCompare.State().Attributes, dInit.State().Attributes)
		}
		if retryIndex >= len(errorCodes)-1 {
			break
		}
	}

	// Update
	patches = gomonkey.ApplyMethod(reflect.TypeOf(&connectivity.AliyunClient{}), "NewResourcemanagerClient", func(_ *connectivity.AliyunClient) (*client.Client, error) {
		return nil, &tea.SDKError{
			Code:       String("loadEndpoint error"),
			Data:       String("loadEndpoint error"),
			Message:    String("loadEndpoint error"),
			StatusCode: tea.Int(400),
		}
	})
	err = resourceAliCloudResourceManagerResourceGroupUpdate(dExisted, rawClient)
	patches.Reset()
	assert.NotNil(t, err)
	// UpdateResourceGroup
	attributesDiff := map[string]interface{}{
		"display_name": "UpdateResourceGroupValue",
	}
	diff, err := newInstanceDiff("alicloud_resource_manager_resource_group", attributes, attributesDiff, dInit.State())
	if err != nil {
		t.Error(err)
	}
	dExisted, _ = schema.InternalMap(p["alicloud_resource_manager_resource_group"].Schema).Data(dInit.State(), diff)
	ReadMockResponseDiff = map[string]interface{}{
		// GetResourceGroup Response
		"ResourceGroup": map[string]interface{}{
			"DisplayName": "UpdateResourceGroupValue",
		},
	}
	errorCodes = []string{"NonRetryableError", "Throttling", "nil"}
	for index, errorCode := range errorCodes {
		retryIndex := index - 1
		patches := gomonkey.ApplyMethod(reflect.TypeOf(&client.Client{}), "DoRequest", func(_ *client.Client, action *string, _ *string, _ *string, _ *string, _ *string, _ map[string]interface{}, _ map[string]interface{}, _ *util.RuntimeOptions) (map[string]interface{}, error) {
			if *action == "UpdateResourceGroup" {
				switch errorCode {
				case "NonRetryableError":
					return failedResponseMock(errorCode)
				default:
					retryIndex++
					if retryIndex >= len(errorCodes)-1 {
						return successResponseMock(ReadMockResponseDiff)
					}
					return failedResponseMock(errorCodes[retryIndex])
				}
			}
			return ReadMockResponse, nil
		})
		err := resourceAliCloudResourceManagerResourceGroupUpdate(dExisted, rawClient)
		patches.Reset()
		switch errorCode {
		case "NonRetryableError":
			assert.NotNil(t, err)
		default:
			assert.Nil(t, err)
			dCompare, _ := schema.InternalMap(p["alicloud_resource_manager_resource_group"].Schema).Data(dExisted.State(), nil)
			for key, value := range attributes {
				dCompare.Set(key, value)
			}
			assert.Equal(t, dCompare.State().Attributes, dExisted.State().Attributes)
		}
		if retryIndex >= len(errorCodes)-1 {
			break
		}
	}

	// Read
	errorCodes = []string{"NonRetryableError", "Throttling", "nil", "{}"}
	for index, errorCode := range errorCodes {
		retryIndex := index - 1
		patches := gomonkey.ApplyMethod(reflect.TypeOf(&client.Client{}), "DoRequest", func(_ *client.Client, action *string, _ *string, _ *string, _ *string, _ *string, _ map[string]interface{}, _ map[string]interface{}, _ *util.RuntimeOptions) (map[string]interface{}, error) {
			if *action == "GetResourceGroup" {
				switch errorCode {
				case "{}":
					return notFoundResponseMock(errorCode)
				case "NonRetryableError":
					return failedResponseMock(errorCode)
				default:
					retryIndex++
					if errorCodes[retryIndex] == "nil" {
						return ReadMockResponse, nil
					}
					return failedResponseMock(errorCodes[retryIndex])
				}
			}
			return ReadMockResponse, nil
		})
		err := resourceAliCloudResourceManagerResourceGroupRead(dExisted, rawClient)
		patches.Reset()
		switch errorCode {
		case "NonRetryableError":
			assert.NotNil(t, err)
		case "{}":
			assert.Nil(t, err)
		}
	}

	// Delete
	patches = gomonkey.ApplyMethod(reflect.TypeOf(&connectivity.AliyunClient{}), "NewResourcemanagerClient", func(_ *connectivity.AliyunClient) (*client.Client, error) {
		return nil, &tea.SDKError{
			Code:       String("loadEndpoint error"),
			Data:       String("loadEndpoint error"),
			Message:    String("loadEndpoint error"),
			StatusCode: tea.Int(400),
		}
	})
	err = resourceAliCloudResourceManagerResourceGroupDelete(dExisted, rawClient)
	patches.Reset()
	assert.NotNil(t, err)
	errorCodes = []string{"NonRetryableError", "Throttling", "nil", "EntityNotExists.ResourceGroup"}
	for index, errorCode := range errorCodes {
		retryIndex := index - 1
		patches := gomonkey.ApplyMethod(reflect.TypeOf(&client.Client{}), "DoRequest", func(_ *client.Client, action *string, _ *string, _ *string, _ *string, _ *string, _ map[string]interface{}, _ map[string]interface{}, _ *util.RuntimeOptions) (map[string]interface{}, error) {
			if *action == "DeleteResourceGroup" {
				switch errorCode {
				case "NonRetryableError", "EntityNotExists.ResourceGroup":
					return failedResponseMock(errorCode)
				default:
					retryIndex++
					if errorCodes[retryIndex] == "nil" {
						ReadMockResponse = map[string]interface{}{}
						return ReadMockResponse, nil
					}
					return failedResponseMock(errorCodes[retryIndex])
				}
			}
			return ReadMockResponse, nil
		})
		err := resourceAliCloudResourceManagerResourceGroupDelete(dExisted, rawClient)
		patches.Reset()
		switch errorCode {
		case "NonRetryableError":
			assert.NotNil(t, err)
		case "EntityNotExists.ResourceGroup":
			assert.Nil(t, err)
		}
	}

}
