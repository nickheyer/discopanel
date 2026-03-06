/** Badge variant type matching the UI component's expected variants. */
type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

const BADGE_VARIANTS: BadgeVariant[] = ['default', 'secondary', 'destructive', 'outline'];

/**
 * Deterministically maps a role name to a badge variant using a simple hash.
 * Works for any dynamic role name without hardcoding specific names.
 */
export function getRoleBadgeVariant(roleName: string): BadgeVariant {
	let hash = 0;
	for (let i = 0; i < roleName.length; i++) {
		hash = ((hash << 5) - hash + roleName.charCodeAt(i)) | 0;
	}
	return BADGE_VARIANTS[Math.abs(hash) % BADGE_VARIANTS.length];
}
