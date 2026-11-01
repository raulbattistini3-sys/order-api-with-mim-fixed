package auth

import (
	"regexp"
	"slices"
)

// The values must match pocketbase/pb_migrations/1730500000_user_roles.js and
// web/src/lib/roles.ts.
var (
	buyingRoles = []string{
		"DECISION_MAKER", "STAKEHOLDER", "BUDGET_HOLDER", "INFLUENCER",
		"CHAMPION", "EXECUTIVE_SPONSOR", "END_USER", "LEGAL_AND_COMPLIANCE",
	}
	jobFunctions = []string{
		"executive", "sales", "marketing", "customer_success", "product", "design",
		"software_engineering", "data_analytics", "it_security", "finance",
		"human_resources", "operations", "procurement", "legal", "other",
	}
)

// e164 is the international format phones are stored in, such as +5511987654321.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// validProfile checks the optional profile fields. An empty value is allowed.
func validProfile(phone, jobFunction string, roles []string) bool {
	if phone != "" && !e164.MatchString(phone) {
		return false
	}
	if jobFunction != "" && !slices.Contains(jobFunctions, jobFunction) {
		return false
	}
	for _, role := range roles {
		if !slices.Contains(buyingRoles, role) {
			return false
		}
	}
	return true
}
