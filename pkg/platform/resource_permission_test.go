package platform_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/jfrog/terraform-provider-platform/pkg/platform"
	"github.com/jfrog/terraform-provider-shared/testutil"
)

func TestAccPermission_full(t *testing.T) {
	_, fqrn, permissionName := testutil.MkNames("test-permission", "platform_permission")
	_, _, userName := testutil.MkNames("test-user", "artifactory_managed_user")
	_, _, groupName := testutil.MkNames("test-group", "artifactory_group")
	_, _, repoName := testutil.MkNames("test-local-repo", "artifactory_local_generic_repository")

	temp := `
	resource "artifactory_managed_user" "{{ .userName }}" {
		name = "{{ .userName }}"
		email = "{{ .userName }}@tempurl.org"
	}

	resource "artifactory_group" "{{ .groupName }}" {
		name = "{{ .groupName }}"
	}

	resource "artifactory_local_generic_repository" "{{ .repoName }}" {
		key = "{{ .repoName }}"
	}

	resource "platform_permission" "{{ .name }}" {
		name = "{{ .name }}"

		artifact = {
			actions = {
				users = [
					{
						name = artifactory_managed_user.{{ .userName }}.name
						permissions = ["READ"]
					}
				]

				groups = [
					{
						name = artifactory_group.{{ .groupName }}.name
						permissions = ["READ"]
					}
				]
			}

			targets = [ 
				{
					name = artifactory_local_generic_repository.{{ .repoName }}.key
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				}
			]
		}
	}`

	updatedTemp := `
	resource "artifactory_managed_user" "{{ .userName }}" {
		name = "{{ .userName }}"
		email = "{{ .userName }}@tempurl.org"
	}

	resource "artifactory_group" "{{ .groupName }}" {
		name = "{{ .groupName }}"
	}

	resource "artifactory_local_generic_repository" "{{ .repoName }}" {
		key = "{{ .repoName }}"
	}

	resource "platform_permission" "{{ .name }}" {
		name = "{{ .name }}"

		artifact = {
			actions = {
				users = [
					{
						name = artifactory_managed_user.{{ .userName }}.name
						permissions = ["READ", "WRITE"]
					}
				]
			}

			targets = [
				{
					name = artifactory_local_generic_repository.{{ .repoName }}.key
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				},
				{
					name = "ALL-LOCAL"
					include_patterns = ["**", "*.js"]
				},
				{
					name = "ALL-REMOTE"
					include_patterns = ["**", "*.js"]
				},
				{
					name = "ALL-DISTRIBUTION"
					include_patterns = ["**", "*.js"]
				}
			]
		}

		build = {
			targets = [
				{
					name = "artifactory-build-info"
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				}
			] 
		}

		release_bundle = {
			actions = {
				users = [
					{
						name = artifactory_managed_user.{{ .userName }}.name
						permissions = ["READ", "WRITE"]
					}
				]

				groups = [
					{
						name = artifactory_group.{{ .groupName }}.name
						permissions = ["READ", "ANNOTATE"]
					}
				]
			}

			targets = [
				{
					name = "release-bundle"
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				}
			]
		}

		destination = {
			actions = {
				groups = [
					{
						name = artifactory_group.{{ .groupName }}.name
						permissions = ["READ", "ANNOTATE"]
					}
				]
			}

			targets = [
				{
					name = "*"
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				}
			]
		}

		pipeline_source = {
			actions = {
				groups = [
					{
						name = artifactory_group.{{ .groupName }}.name
						permissions = ["READ", "ANNOTATE"]
					}
				]
			}

			targets = [
				{
					name = "*"
					include_patterns = ["**"]
					exclude_patterns = ["{{ .excludePattern }}"]
				}
			]
		}
	}`

	testData := map[string]string{
		"name":           permissionName,
		"userName":       userName,
		"groupName":      groupName,
		"repoName":       repoName,
		"excludePattern": "foo",
	}

	config := testutil.ExecuteTemplate(permissionName, temp, testData)

	updatedTestData := map[string]string{
		"name":           permissionName,
		"userName":       userName,
		"groupName":      groupName,
		"repoName":       repoName,
		"excludePattern": "bar",
	}
	updatedConfig := testutil.ExecuteTemplate(permissionName, updatedTemp, updatedTestData)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders(),
		ExternalProviders: map[string]resource.ExternalProvider{
			"artifactory": {
				Source: "jfrog/artifactory",
			},
		},
		CheckDestroy: testAccCheckPermissionDestroy(fqrn),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(fqrn, "name", testData["name"]),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.users.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.users.0.name", testData["userName"]),
					resource.TestCheckTypeSetElemAttr(fqrn, "artifact.actions.users.0.permissions.*", "READ"),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.groups.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.groups.0.name", testData["groupName"]),
					resource.TestCheckTypeSetElemAttr(fqrn, "artifact.actions.groups.0.permissions.*", "READ"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.0.name", testData["repoName"]),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.0.include_patterns.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.0.include_patterns.0", "**"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.0.exclude_patterns.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.0.exclude_patterns.0", testData["excludePattern"]),
				),
			},
			{
				Config: updatedConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(fqrn, "name", updatedTestData["name"]),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.users.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.users.0.name", testData["userName"]),
					resource.TestCheckResourceAttr(fqrn, "artifact.actions.users.0.permissions.#", "2"),
					resource.TestCheckTypeSetElemAttr(fqrn, "artifact.actions.users.0.permissions.*", "READ"),
					resource.TestCheckTypeSetElemAttr(fqrn, "artifact.actions.users.0.permissions.*", "WRITE"),
					resource.TestCheckNoResourceAttr(fqrn, "artifact.actions.groups"),
					resource.TestCheckResourceAttr(fqrn, "artifact.targets.#", "4"),
					resource.TestCheckNoResourceAttr(fqrn, "build.actions"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.0.name", "artifactory-build-info"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.0.include_patterns.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.0.include_patterns.0", "**"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.0.exclude_patterns.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "build.targets.0.exclude_patterns.0", updatedTestData["excludePattern"]),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.actions.users.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.actions.users.0.permissions.#", "2"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.actions.users.0.permissions.*", "READ"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.actions.users.0.permissions.*", "WRITE"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.actions.groups.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.actions.groups.0.permissions.#", "2"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.actions.groups.0.permissions.*", "READ"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.actions.groups.0.permissions.*", "ANNOTATE"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.targets.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.targets.0.name", "release-bundle"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.targets.0.include_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.targets.0.include_patterns.*", "**"),
					resource.TestCheckResourceAttr(fqrn, "release_bundle.targets.0.exclude_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "release_bundle.targets.0.exclude_patterns.*", updatedTestData["excludePattern"]),
					resource.TestCheckResourceAttr(fqrn, "destination.actions.groups.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "destination.actions.groups.0.permissions.#", "2"),
					resource.TestCheckTypeSetElemAttr(fqrn, "destination.actions.groups.0.permissions.*", "READ"),
					resource.TestCheckTypeSetElemAttr(fqrn, "destination.actions.groups.0.permissions.*", "ANNOTATE"),
					resource.TestCheckResourceAttr(fqrn, "destination.targets.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "destination.targets.0.name", "*"),
					resource.TestCheckResourceAttr(fqrn, "destination.targets.0.include_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "destination.targets.0.include_patterns.*", "**"),
					resource.TestCheckResourceAttr(fqrn, "destination.targets.0.exclude_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "destination.targets.0.exclude_patterns.*", updatedTestData["excludePattern"]),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.actions.groups.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.actions.groups.0.permissions.#", "2"),
					resource.TestCheckTypeSetElemAttr(fqrn, "pipeline_source.actions.groups.0.permissions.*", "READ"),
					resource.TestCheckTypeSetElemAttr(fqrn, "pipeline_source.actions.groups.0.permissions.*", "ANNOTATE"),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.targets.#", "1"),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.targets.0.name", "*"),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.targets.0.include_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "pipeline_source.targets.0.include_patterns.*", "**"),
					resource.TestCheckResourceAttr(fqrn, "pipeline_source.targets.0.exclude_patterns.#", "1"),
					resource.TestCheckTypeSetElemAttr(fqrn, "pipeline_source.targets.0.exclude_patterns.*", updatedTestData["excludePattern"]),
				),
			},
			{
				ResourceName:                         fqrn,
				ImportState:                          true,
				ImportStateId:                        permissionName,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

func testAccCheckPermissionDestroy(id string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		client := TestProvider.(*platform.PlatformProvider).Meta.Client

		rs, ok := s.RootModule().Resources[id]
		if !ok {
			return fmt.Errorf("err: Resource id[%s] not found", id)
		}

		var permission platform.PermissionAPIModel
		url, err := url.JoinPath(platform.PermissionEndpoint, rs.Primary.Attributes["name"])
		if err != nil {
			return err
		}

		resp, err := client.R().
			SetResult(&permission).
			Get(url)

		if err != nil {
			return err
		}

		if resp != nil && resp.StatusCode() == http.StatusNotFound {
			return nil
		}

		return fmt.Errorf("error: Permission %s still exists", rs.Primary.Attributes["name"])
	}
}
