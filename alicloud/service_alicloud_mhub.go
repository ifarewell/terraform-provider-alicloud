package alicloud

import (
	"fmt"

	"time"

	"github.com/PaesslerAG/jsonpath"
	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
)

type MhubService struct {
	client *connectivity.AliyunClient
}

func (s *MhubService) DescribeMhubProduct(id string) (object map[string]interface{}, err error) {
	var response map[string]interface{}
	client := s.client
	action := "QueryProductInfo"
	request := map[string]interface{}{
		"ProductId": id,
	}
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		response, err = client.RpcPost("Mhub", "2017-08-25", action, nil, request, true)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})
	addDebug(action, response, request)
	if err != nil {

		if IsExpectedErrors(err, []string{"ProductNotExist"}) {
			err = WrapErrorf(NotFoundErr("MHUBProduct", id), NotFoundMsg, ProviderERROR)
			return object, err
		}
		return object, WrapErrorf(err, DefaultErrorMsg, id, action, AlibabaCloudSdkGoERROR)
	}

	v, err := jsonpath.Get("$.ProductInfo", response)
	if err != nil {
		return object, WrapErrorf(err, FailedGetAttributeMsg, id, "$.ProductInfo", response)
	}
	object = v.(map[string]interface{})
	return object, nil
}

func (s *MhubService) DescribeMhubApp(id string) (object map[string]interface{}, err error) {
	var response map[string]interface{}
	client := s.client
	action := "ListApps"
	idExist := false
	parts, err := ParseResourceId(id, 2)
	request := map[string]interface{}{
		"ProductId": parts[0],
	}
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		response, err = client.RpcPost("Mhub", "2017-08-25", action, nil, request, true)
		if err != nil {
			if NeedRetry(err) {
				wait()
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})
	addDebug(action, response, request)
	if err != nil {
		if IsExpectedErrors(err, []string{"ProductNotExist"}) {
			err = WrapErrorf(NotFoundErr("MHUBProduct", id), NotFoundMsg, ProviderERROR)
			return object, err
		}
		return object, WrapErrorf(err, DefaultErrorMsg, id, action, AlibabaCloudSdkGoERROR)
	}
	v, err := jsonpath.Get("$.AppInfos.AppInfo", response)
	if err != nil {
		return object, WrapErrorf(err, FailedGetAttributeMsg, id, "$.AppInfos.AppInfo", response)
	}
	if len(v.([]interface{})) < 1 {
		return object, WrapErrorf(NotFoundErr("MHUB", id), NotFoundWithResponse, response)
	}
	for _, v := range v.([]interface{}) {
		if fmt.Sprint(v.(map[string]interface{})["AppKey"]) == parts[1] {
			idExist = true
			return v.(map[string]interface{}), nil
		}
	}
	if !idExist {
		return object, WrapErrorf(NotFoundErr("MHUB", id), NotFoundWithResponse, response)
	}

	object = v.(map[string]interface{})
	return object, nil
}
