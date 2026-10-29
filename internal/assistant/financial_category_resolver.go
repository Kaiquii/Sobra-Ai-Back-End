package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"strings"
	"unicode"
)

// Resolve only within the authenticated catalog. Exact names win; a shortened
// name such as Uber may match Uber/99 only when it identifies one owned entry.
func resolveFinancialCategoryName(userID uint, hint string, owned []categories.Category) (uint, string, error) {
	hint = strings.TrimSpace(hint)
	if userID == 0 || hint == "" {
		return 0, "", queryClarification("category_not_found", "Qual categoria cadastrada deseja consultar?")
	}
	var exact, shortened []categories.Category
	requested := financialCategoryWords(hint)
	for _, category := range owned {
		if category.UserID != userID || category.ID == 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(category.Name), hint) {
			exact = append(exact, category)
			continue
		}
		candidate := financialCategoryWords(category.Name)
		if len(requested) == 0 || len(requested[0]) < 2 {
			continue
		}
		for start := 0; start+len(requested) <= len(candidate); start++ {
			if strings.Join(candidate[start:start+len(requested)], " ") == strings.Join(requested, " ") {
				shortened = append(shortened, category)
				break
			}
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = shortened
	}
	if len(matches) == 1 {
		return matches[0].ID, matches[0].Name, nil
	}
	if len(matches) > 1 {
		return 0, "", queryClarification("category_ambiguous", "Ha mais de uma categoria sua correspondente a esse nome. Qual delas deseja consultar?")
	}
	return 0, "", queryClarification("category_not_found", "Nao encontrei uma categoria sua correspondente a esse nome. Qual categoria cadastrada deseja consultar?")
}

func financialCategoryWords(value string) []string {
	value = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c").Replace(strings.ToLower(value))
	return strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
