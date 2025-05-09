package alicloud

import (
	"fmt"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"

	"github.com/PaesslerAG/jsonpath"
	"github.com/aliyun/terraform-provider-alicloud/alicloud/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func dataSourceAlicloudCrChartRepositories() *schema.Resource {
	return &schema.Resource{
		Read: dataSourceAlicloudCrChartRepositoriesRead,
		Schema: map[string]*schema.Schema{
			"instance_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"name_regex": {
				Type:         schema.TypeString,
				Optional:     true,
				ValidateFunc: validation.ValidateRegexp,
			},
			"names": {
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"ids": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Computed: true,
			},
			"output_file": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"repositories": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"chart_repository_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"create_time": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"instance_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"repo_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"repo_namespace_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"repo_type": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"summary": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func dataSourceAlicloudCrChartRepositoriesRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AliyunClient)

	action := "ListChartRepository"
	request := make(map[string]interface{})
	request["InstanceId"] = d.Get("instance_id")
	request["RegionId"] = client.RegionId

	var objects []map[string]interface{}

	idsMap := make(map[string]string)
	if v, ok := d.GetOk("ids"); ok {
		for _, vv := range v.([]interface{}) {
			if vv == nil {
				continue
			}
			idsMap[vv.(string)] = vv.(string)
		}
	}
	var nameRegex *regexp.Regexp
	if v, ok := d.GetOk("name_regex"); ok {
		nameRegex = regexp.MustCompile(v.(string))
	}

	var response map[string]interface{}
	var err error
	pageNo, pageSize := 1, PageSizeLarge
	for {
		wait := incrementalWait(3*time.Second, 3*time.Second)
		err = resource.Retry(5*time.Minute, func() *resource.RetryError {
			response, err = client.RpcPost("cr", "2018-12-01", action, nil, request, true)
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
			return WrapErrorf(err, DataDefaultErrorMsg, "alicloud_cr_chart_repositories", action, AlibabaCloudSdkGoERROR)
		}
		resp, err := jsonpath.Get("$.Repositories", response)
		if err != nil {
			return WrapErrorf(err, FailedGetAttributeMsg, action, "$.Repositories", response)
		}
		result, _ := resp.([]interface{})
		for _, v := range result {
			item := v.(map[string]interface{})
			if nameRegex != nil && !nameRegex.MatchString(fmt.Sprint(item["RepoName"])) {
				continue
			}
			if len(idsMap) > 0 {
				if _, ok := idsMap[fmt.Sprint(item["InstanceId"], ":", item["RepoNamespaceName"], ":", item["RepoName"])]; !ok {
					continue
				}
			}
			objects = append(objects, item)
		}
		if len(result) < pageSize {
			break
		}
		pageNo++
	}
	ids := make([]string, 0)
	names := make([]interface{}, 0)
	s := make([]map[string]interface{}, 0)

	for _, object := range objects {
		mapping := map[string]interface{}{
			"chart_repository_id": object["RepoId"],
			"create_time":         fmt.Sprint(object["CreateTime"]),
			"instance_id":         object["InstanceId"],
			"id":                  fmt.Sprint(object["InstanceId"], ":", object["RepoNamespaceName"], ":", object["RepoName"]),
			"repo_name":           fmt.Sprint(object["RepoName"]),
			"repo_namespace_name": object["RepoNamespaceName"],
			"repo_type":           object["RepoType"],
			"summary":             object["Summary"],
		}
		ids = append(ids, fmt.Sprint(mapping["id"]))
		names = append(names, mapping["repo_name"])
		s = append(s, mapping)
	}

	d.SetId(dataResourceIdHash(ids))
	if err := d.Set("ids", ids); err != nil {
		return WrapError(err)
	}
	if err := d.Set("names", names); err != nil {
		return WrapError(err)
	}

	if err := d.Set("repositories", s); err != nil {
		return WrapError(err)
	}
	if output, ok := d.GetOk("output_file"); ok && output.(string) != "" {
		writeToFile(output.(string), s)
	}

	return nil
}
