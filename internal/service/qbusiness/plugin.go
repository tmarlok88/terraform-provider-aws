// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package qbusiness

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/YakDriver/regexache"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/qbusiness"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

func authConfigurationSchema() *schema.Schema {
	return &schema.Schema{
		Type:         schema.TypeList,
		Optional:     true,
		MaxItems:     1,
		ExactlyOneOf: []string{"basic_auth_configuration", "oauth2_client_credential_configuration"},
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"role_arn": {
					Type:         schema.TypeString,
					Required:     true,
					Description:  "ARN of an IAM role used by Amazon Q to access the basic authentication credentials stored in a Secrets Manager secret.",
					ValidateFunc: verify.ValidARN,
				},
				"secret_arn": {
					Type:         schema.TypeString,
					Required:     true,
					Description:  "ARN of the Secrets Manager secret that stores the basic authentication credentials used for plugin configuration.",
					ValidateFunc: verify.ValidARN,
				},
			},
		},
	}

}

// @SDKResource("aws_qbusiness_plugin", name="Plugin")
// @Tags(identifierAttribute="arn")
func ResourcePlugin() *schema.Resource {
	return &schema.Resource{

		CreateWithoutTimeout: resourcePluginCreate,
		ReadWithoutTimeout:   resourcePluginRead,
		UpdateWithoutTimeout: resourcePluginUpdate,
		DeleteWithoutTimeout: resourcePluginDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		CustomizeDiff: verify.SetTagsDiff,

		Schema: map[string]*schema.Schema{
			"application_id": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Identifier of the Amazon Q application associated with the plugin.",
				ValidateFunc: validation.All(
					validation.StringMatch(regexache.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{35}$`), "must be a valid application ID"),
				),
			},
			"arn": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "ARN of the Amazon Q plugin.",
			},
			"basic_auth_configuration":               authConfigurationSchema(),
			"oauth2_client_credential_configuration": authConfigurationSchema(),
			"display_name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the Amazon Q plugin.",
				ValidateFunc: validation.All(
					validation.StringLenBetween(1, 100),
					validation.StringMatch(regexache.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`), "must begin with a letter or number and contain only alphanumeric, underscore, or hyphen characters"),
				),
			},
			"plugin_id": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The identifier of the Amazon Q plugin.",
			},
			"server_url": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Source URL used for plugin configuration.",
				ValidateFunc: validation.All(
					validation.StringLenBetween(1, 2048),
					validation.StringMatch(regexache.MustCompile(`^(https?|ftp|file)://([^\s]*)$`), "must be a valid URL"),
				),
			},
			"state": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "State of plugin. Valid value are `ENABLED` and `DISABLED`",
				ValidateFunc: validation.StringInSlice(qbusiness.PluginState_Values(), false),
			},
			"type": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "Type of plugin. Valid value are `SERVICE_NOW`, `SALESFORCE`, `JIRA`, and `ZENDESK`",
				ValidateFunc: validation.StringInSlice(qbusiness.PluginType_Values(), false),
			},
			names.AttrTags:    tftags.TagsSchema(),
			names.AttrTagsAll: tftags.TagsSchemaComputed(),
		},
	}
}

func resourcePluginCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).QBusinessConn(ctx)

	application_id := d.Get("application_id").(string)

	input := &qbusiness.CreatePluginInput{
		ApplicationId: aws.String(application_id),
		DisplayName:   aws.String(d.Get("display_name").(string)),
		ServerUrl:     aws.String(d.Get("server_url").(string)),
		Type:          aws.String(d.Get("type").(string)),
		AuthConfiguration: &qbusiness.PluginAuthConfiguration{
			BasicAuthConfiguration:              expandBasicAuthConfiguration(d.Get("basic_auth_configuration").([]interface{})),
			OAuth2ClientCredentialConfiguration: expandOAuth2ClientCredentialConfiguration(d.Get("oauth2_client_credential_configuration").([]interface{})),
		},
		Tags: getTagsIn(ctx),
	}

	output, err := conn.CreatePlugin(input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating qbusiness plugin: %s", err)
	}

	d.SetId(application_id + "/" + aws.ToString(output.PluginId))

	updateInput := &qbusiness.UpdatePluginInput{
		ApplicationId: aws.String(application_id),
		PluginId:      output.PluginId,
		State:         aws.String(d.Get("state").(string)),
	}

	_, err = conn.UpdatePlugin(updateInput)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "updating qbusiness plugin: %s", err)
	}

	return append(diags, resourceIndexRead(ctx, d, meta)...)
}

func resourcePluginRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).QBusinessConn(ctx)

	output, err := FindPluginByID(ctx, conn, d.Id())

	if !d.IsNewResource() && tfawserr.ErrCodeEquals(err, qbusiness.ErrCodeResourceNotFoundException) {
		log.Printf("[WARN] qbusiness plugin (%s) not found, removing from state", d.Id())
		d.SetId("")
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading qbusiness plugin (%s): %s", d.Id(), err)
	}

	d.Set("application_id", output.ApplicationId)
	d.Set("arn", output.PluginArn)
	d.Set("display_name", output.DisplayName)

	if output.AuthConfiguration != nil {
		if err := d.Set("basic_auth_configuration",
			flattenBasicAuthConfiguration(output.AuthConfiguration.BasicAuthConfiguration)); err != nil {
			return sdkdiag.AppendErrorf(diags, "setting qbusiness plugin basic_auth_configuration: %s", err)
		}
		if err := d.Set("oauth2_client_credential_configuration",
			flattenOAuth2ClientCredentialConfiguration(output.AuthConfiguration.OAuth2ClientCredentialConfiguration)); err != nil {
			return sdkdiag.AppendErrorf(diags, "setting qbusiness plugin oauth2_client_credential_configuration: %s", err)
		}
	}

	d.Set("plugin_id", output.PluginId)
	d.Set("server_url", output.ServerUrl)
	d.Set("state", output.State)
	d.Set("type", output.Type)

	return diags
}

func resourcePluginUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).QBusinessConn(ctx)

	application_id, plugin_id, err := parsePluginID(d.Id())

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "parsing qbusiness plugin ID (%s): %s", d.Id(), err)
	}

	input := &qbusiness.UpdatePluginInput{
		ApplicationId: aws.String(application_id),
		PluginId:      aws.String(plugin_id),
	}

	if d.HasChange("display_name") {
		input.DisplayName = aws.String(d.Get("display_name").(string))
	}
	if d.HasChange("server_url") {
		input.ServerUrl = aws.String(d.Get("server_url").(string))
	}
	if d.HasChange("state") {
		input.State = aws.String(d.Get("state").(string))
	}

	_, err = conn.UpdatePlugin(input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "updating qbusiness plugin: %s", err)
	}

	return append(diags, resourcePluginRead(ctx, d, meta)...)
}

func resourcePluginDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).QBusinessConn(ctx)

	application_id, plugin_id, err := parsePluginID(d.Id())

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "parsing qbusiness plugin ID (%s): %s", d.Id(), err)
	}

	_, err = conn.DeletePlugin(&qbusiness.DeletePluginInput{
		ApplicationId: aws.String(application_id),
		PluginId:      aws.String(plugin_id),
	})

	if tfawserr.ErrCodeEquals(err, qbusiness.ErrCodeResourceNotFoundException) {
		return nil
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting qbusiness plugin (%s): %s", d.Id(), err)
	}

	return diags
}

func parsePluginID(id string) (string, string, error) {
	parts := strings.Split(id, "/")

	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid plugin ID: %s", id)
	}

	return parts[0], parts[1], nil
}

func FindPluginByID(ctx context.Context, conn *qbusiness.QBusiness, id string) (*qbusiness.GetPluginOutput, error) {
	application_id, plugin_id, err := parsePluginID(id)

	if err != nil {
		return nil, err
	}

	input := &qbusiness.GetPluginInput{
		ApplicationId: aws.String(application_id),
		PluginId:      aws.String(plugin_id),
	}

	output, err := conn.GetPlugin(input)

	if tfawserr.ErrCodeEquals(err, qbusiness.ErrCodeResourceNotFoundException) {
		return nil, &retry.NotFoundError{
			LastError:   err,
			LastRequest: input,
		}
	}

	if err != nil {
		return nil, err
	}

	if output == nil {
		return nil, tfresource.NewEmptyResultError(input)
	}

	return output, nil
}

func flattenBasicAuthConfiguration(basicAuthConfiguration *qbusiness.BasicAuthConfiguration) []interface{} {
	if basicAuthConfiguration == nil {
		return []interface{}{}
	}
	return []interface{}{
		map[string]interface{}{
			"role_arn":   aws.ToString(basicAuthConfiguration.RoleArn),
			"secret_arn": aws.ToString(basicAuthConfiguration.SecretArn),
		},
	}
}

func flattenOAuth2ClientCredentialConfiguration(oauth2ClientCredentialConfiguration *qbusiness.OAuth2ClientCredentialConfiguration) []interface{} {
	if oauth2ClientCredentialConfiguration == nil {
		return []interface{}{}
	}
	return []interface{}{
		map[string]interface{}{
			"role_arn":   aws.ToString(oauth2ClientCredentialConfiguration.RoleArn),
			"secret_arn": aws.ToString(oauth2ClientCredentialConfiguration.SecretArn),
		},
	}
}

func expandBasicAuthConfiguration(basicAuthConfiguration []interface{}) *qbusiness.BasicAuthConfiguration {
	if len(basicAuthConfiguration) == 0 {
		return nil
	}

	basicAuth := basicAuthConfiguration[0].(map[string]interface{})

	return &qbusiness.BasicAuthConfiguration{
		RoleArn:   aws.String(basicAuth["role_arn"].(string)),
		SecretArn: aws.String(basicAuth["secret_arn"].(string)),
	}
}

func expandOAuth2ClientCredentialConfiguration(oauth2ClientCredentialConfiguration []interface{}) *qbusiness.OAuth2ClientCredentialConfiguration {
	if len(oauth2ClientCredentialConfiguration) == 0 {
		return nil
	}

	oAuth2 := oauth2ClientCredentialConfiguration[0].(map[string]interface{})

	return &qbusiness.OAuth2ClientCredentialConfiguration{
		RoleArn:   aws.String(oAuth2["role_arn"].(string)),
		SecretArn: aws.String(oAuth2["secret_arn"].(string)),
	}
}
