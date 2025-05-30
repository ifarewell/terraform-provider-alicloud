package alicloud

import (
	"fmt"
	"log"
	"time"

	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func resourceAliCloudResourceManagerSharedResource() *schema.Resource {
	return &schema.Resource{
		Create: resourceAliCloudResourceManagerSharedResourceCreate,
		Read:   resourceAliCloudResourceManagerSharedResourceRead,
		Delete: resourceAliCloudResourceManagerSharedResourceDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(11 * time.Minute),
			Delete: schema.DefaultTimeout(11 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"resource_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"resource_share_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"resource_type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"status": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceAliCloudResourceManagerSharedResourceCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AliyunClient)
	resourcesharingService := ResourcesharingService{client}
	var response map[string]interface{}
	action := "AssociateResourceShare"
	request := make(map[string]interface{})
	var err error
	request["Resources.1.ResourceId"] = d.Get("resource_id")
	request["ResourceShareId"] = d.Get("resource_share_id")
	request["Resources.1.ResourceType"] = d.Get("resource_type")

	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(client.GetRetryTimeout(d.Timeout(schema.TimeoutCreate)), func() *resource.RetryError {
		response, err = client.RpcPost("ResourceSharing", "2020-01-10", action, nil, request, false)
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
		return WrapErrorf(err, DefaultErrorMsg, "alicloud_resource_manager_shared_resource", action, AlibabaCloudSdkGoERROR)
	}

	response = response["ResourceShareAssociations"].([]interface{})[0].(map[string]interface{})
	d.SetId(fmt.Sprint(response["ResourceShareId"], ":", response["EntityId"], ":", response["EntityType"]))

	stateConf := BuildStateConf([]string{}, []string{"Associated"}, d.Timeout(schema.TimeoutCreate), 5*time.Second, resourcesharingService.ResourceManagerSharedResourceStateRefreshFunc(d.Id(), []string{"Failed"}))
	if _, err := stateConf.WaitForState(); err != nil {
		return WrapErrorf(err, IdMsg, d.Id())
	}

	return resourceAliCloudResourceManagerSharedResourceRead(d, meta)
}

func resourceAliCloudResourceManagerSharedResourceRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AliyunClient)
	resourcesharingService := ResourcesharingService{client}
	object, err := resourcesharingService.DescribeResourceManagerSharedResource(d.Id())
	if err != nil {
		if !d.IsNewResource() && NotFoundError(err) {
			log.Printf("[DEBUG] Resource alicloud_resource_manager_shared_resource resourcesharingService.DescribeResourceManagerSharedResource Failed!!! %s", err)
			d.SetId("")
			return nil
		}
		return WrapError(err)
	}

	parts, err := ParseResourceId(d.Id(), 3)
	if err != nil {
		return WrapError(err)
	}

	d.Set("resource_id", parts[1])
	d.Set("resource_share_id", parts[0])
	d.Set("resource_type", parts[2])
	d.Set("status", object["AssociationStatus"])

	return nil
}

func resourceAliCloudResourceManagerSharedResourceDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AliyunClient)
	parts, err := ParseResourceId(d.Id(), 3)
	if err != nil {
		return WrapError(err)
	}
	resourcesharingService := ResourcesharingService{client}
	action := "DisassociateResourceShare"
	var response map[string]interface{}
	request := map[string]interface{}{
		"Resources.1.ResourceId":   parts[1],
		"ResourceShareId":          parts[0],
		"Resources.1.ResourceType": parts[2],
	}

	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(client.GetRetryTimeout(d.Timeout(schema.TimeoutDelete)), func() *resource.RetryError {
		response, err = client.RpcPost("ResourceSharing", "2020-01-10", action, nil, request, false)
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
		return WrapErrorf(err, DefaultErrorMsg, d.Id(), action, AlibabaCloudSdkGoERROR)
	}

	stateConf := BuildStateConf([]string{}, []string{"Disassociated"}, d.Timeout(schema.TimeoutDelete), 5*time.Second, resourcesharingService.ResourceManagerSharedResourceStateRefreshFunc(d.Id(), []string{"Failed"}))
	if _, err := stateConf.WaitForState(); err != nil {
		return WrapErrorf(err, IdMsg, d.Id())
	}

	return nil
}
