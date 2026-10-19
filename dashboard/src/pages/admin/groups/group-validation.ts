import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { AppGroupView, UpdateGroupRequest } from "@/api/raw-admin-paths";

/** The checks a group's name and identifier get before anything is sent. */

const slugPattern = /^[a-z0-9](-?[a-z0-9])*$/;
const maxSlugLength = 64;

export function validateGroupName(
  value: string,
): MessageDescriptor | undefined {
  return value.trim() === ""
    ? msg({
        id: "admin.group.display-name.required",
        message: "Give the group a name.",
      })
    : undefined;
}

/**
 * Mirrors `validateAppGroupSlug`: lowercase letters, digits and single hyphens,
 * at most 64 characters. The server answers a malformed slug with a bare
 * `bad_request`, which says nothing useful, so the shape is stated here instead.
 */
export function validateGroupSlug(
  value: string,
): MessageDescriptor | undefined {
  const slug = value.trim();
  if (slug === "") {
    return msg({
      id: "admin.group.slug.required",
      message: "Give the group an identifier.",
    });
  }
  return slugPattern.test(slug) && slug.length <= maxSlugLength
    ? undefined
    : msg({
        id: "admin.group.slug.format",
        message:
          "Use lowercase letters, digits and hyphens, up to 64 characters.",
      });
}

/**
 * The request that saves a group, built from the stored record.
 *
 * The update replaces the record, so a section that edits one field still sends
 * the others as they are stored. The rule is left out on purpose: an omitted
 * rule means "keep the current one", and a manual group has none to send.
 */
export function groupUpdateBody(
  group: AppGroupView,
  changes: Partial<UpdateGroupRequest>,
): UpdateGroupRequest {
  return {
    slug: group.slug,
    displayName: group.displayName,
    description: group.description ?? "",
    exposedToDownstream: group.exposedToDownstream,
    ...changes,
  };
}
