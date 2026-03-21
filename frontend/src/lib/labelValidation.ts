const reservedPrefixes = ["com.docker.", "io.docker.", "org.dockerproject."];

/**
 * Returns true if the label key belongs to a reserved Docker namespace
 * and should not be modified by users.
 *
 * @see https://docs.docker.com/engine/manage-resources/labels/#key-format-recommendations
 */
export function isReservedLabelKey(key: string): boolean {
  return reservedPrefixes.some((prefix) => key.startsWith(prefix));
}

const labelKeyPattern = /^[a-z0-9]([a-z0-9.-/]*[a-z0-9])?$/;
const consecutiveSeparators = /[.-]{2}/;

/**
 * Validate a Docker label key per the official format recommendations.
 * Returns an error message, or null if valid.
 *
 * Rules:
 * - Must begin and end with a lowercase alphanumeric character
 * - May contain lowercase alphanumeric characters, periods, hyphens, and slashes
 * - No consecutive periods or hyphens
 * - Cannot use a reserved Docker namespace
 *
 * @see https://docs.docker.com/engine/manage-resources/labels/#key-format-recommendations
 */
export function validateLabelKey(key: string): string | null {
  // Return null for empty input to avoid showing errors while the user is still typing.
  if (!key) {
    return null;
  }

  if (isReservedLabelKey(key)) {
    return "Keys starting with com.docker., io.docker., or org.dockerproject. are reserved.";
  }

  if (!labelKeyPattern.test(key)) {
    return "Keys must start and end with a lowercase letter or digit, and contain only lowercase alphanumeric characters, periods, hyphens, or slashes.";
  }

  if (consecutiveSeparators.test(key)) {
    return "Consecutive periods or hyphens are not allowed.";
  }

  return null;
}
