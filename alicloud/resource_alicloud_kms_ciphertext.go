package alicloud

import (
	"strconv"
	"time"

	"github.com/PaesslerAG/jsonpath"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"

	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func resourceAlicloudKmsCiphertext() *schema.Resource {
	return &schema.Resource{
		Create: resourceAlicloudKmsCiphertextCreate,
		Read:   schema.Noop,
		Delete: resourceAlicloudKmsCiphertextDelete,

		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"plaintext": {
				Type:      schema.TypeString,
				Required:  true,
				ForceNew:  true,
				Sensitive: true,
			},
			"key_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"encryption_context": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},
			"ciphertext_blob": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceAlicloudKmsCiphertextCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AliyunClient)

	// Since a ciphertext has no ID, we create an ID based on
	// current unix time.
	d.SetId(strconv.FormatInt(time.Now().Unix(), 16))

	action := "Encrypt"
	request := make(map[string]interface{})
	request["Plaintext"] = d.Get("plaintext")
	request["KeyId"] = d.Get("key_id")
	request["RegionId"] = client.RegionId

	if context := d.Get("encryption_context"); context != nil {
		cm := context.(map[string]interface{})
		contextJson, err := convertMaptoJsonString(cm)
		if err != nil {
			return WrapErrorf(err, DefaultErrorMsg, "alicloud_kms_ciphertext", action, AlibabaCloudSdkGoERROR)
		}
		request["EncryptionContext"] = contextJson
	}

	var response map[string]interface{}
	var err error
	wait := incrementalWait(3*time.Second, 3*time.Second)
	err = resource.Retry(5*time.Minute, func() *resource.RetryError {
		response, err = client.RpcPost("Kms", "2016-01-20", action, nil, request, false)
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
		return WrapErrorf(err, DefaultErrorMsg, "alicloud_kms_ciphertext", action, AlibabaCloudSdkGoERROR)
	}
	resp, err := jsonpath.Get("$.CiphertextBlob", response)
	if err != nil {
		return WrapErrorf(err, FailedGetAttributeMsg, action, "$.CiphertextBlob", response)
	}
	d.Set("ciphertext_blob", resp)

	return nil
}

func resourceAlicloudKmsCiphertextDelete(d *schema.ResourceData, meta interface{}) error {
	return nil
}
