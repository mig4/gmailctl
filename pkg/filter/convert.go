package filter

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/mbrt/gmailctl/pkg/config"
)

// FromConfig translates a config into entries that map directly into Gmail filters
func FromConfig(cfg config.Config) (Filters, error) {
	res := Filters{}
	for i, rule := range cfg.Rules {
		entries, err := FromConfigRule(rule, cfg.Consts)
		if err != nil {
			return res, errors.Wrap(err, fmt.Sprintf("error generating rule #%d", i))
		}
		res = append(res, entries...)
	}
	return res, nil
}

// FromConfigRule creates a set of filters based on a single config Rule
func FromConfigRule(rule config.Rule, consts config.Consts) (Filters, error) {
	rule, err := resolveRuleConsts(rule, consts)
	if err != nil {
		return nil, errors.Wrap(err, "error resolving consts")
	}
	criteria := generateCriteria(rule.Filters)
	actions := generateActions(rule.Actions)
	return combineCriteriaWithActions(criteria, actions), nil
}

// resolveRuleConsts resolves the sections with consts with their respective values
func resolveRuleConsts(cfg config.Rule, consts config.Consts) (config.Rule, error) {
	res := config.Rule{
		// Actions don't need to be resolved
		Actions: cfg.Actions,
	}

	// Resolve the consts
	cm, err := resolveFiltersConsts(cfg.Filters.Consts.MatchFilters, consts)
	if err != nil {
		return res, err
	}
	ncm, err := resolveFiltersConsts(cfg.Filters.Consts.Not, consts)
	if err != nil {
		return res, err
	}

	// Join the non const configuration with the resolved one
	res.Filters.MatchFilters = joinMatchFilters(cfg.Filters.MatchFilters, cm)
	res.Filters.Not = joinMatchFilters(cfg.Filters.Not, ncm)

	return res, nil
}

func resolveFiltersConsts(mf config.MatchFilters, consts config.Consts) (config.MatchFilters, error) {
	from, err := resolveConsts(mf.From, consts)
	if err != nil {
		return mf, errors.Wrap(err, "error in resolving 'from' clause")
	}
	to, err := resolveConsts(mf.To, consts)
	if err != nil {
		return mf, errors.Wrap(err, "error in resolving 'to' clause")
	}
	sub, err := resolveConsts(mf.Subject, consts)
	if err != nil {
		return mf, errors.Wrap(err, "error in resolving 'subject' clause")
	}
	has, err := resolveConsts(mf.Has, consts)
	if err != nil {
		return mf, errors.Wrap(err, "error in resolving 'has' clause")
	}
	list, err := resolveConsts(mf.List, consts)
	if err != nil {
		return mf, errors.Wrap(err, "error in resolving 'list' clause")
	}
	res := config.MatchFilters{
		From:    from,
		To:      to,
		Subject: sub,
		Has:     has,
		List:    list,
	}
	return res, nil
}

func resolveConsts(a []string, consts config.Consts) ([]string, error) {
	res := []string{}
	for _, s := range a {
		resolved, ok := consts[s]
		if !ok {
			return nil, errors.Errorf("failed to resolve const '%s'", s)
		}
		res = append(res, resolved.Values...)
	}
	return res, nil
}

func joinMatchFilters(f1, f2 config.MatchFilters) config.MatchFilters {
	res := config.MatchFilters{}
	res.From = joinFilter(f1.From, f2.From)
	res.To = joinFilter(f1.To, f2.To)
	res.Subject = joinFilter(f1.Subject, f2.Subject)
	res.Has = joinFilter(f1.Has, f2.Has)
	res.List = joinFilter(f1.List, f2.List)
	return res
}

func joinFilter(f1, f2 []string) []string {
	res := []string{}
	res = append(res, f1...)
	res = append(res, f2...)
	return res
}

func generateCriteria(filters config.Filters) Criteria {
	// We can assume that all the consts have been resolved at this point
	// so we can ignore the 'Consts' sections of the filter
	res := generateMatchFilters(filters.MatchFilters)
	negated := generateNegatedFilters(filters.Not)

	// We need to combine the negated query, 'has' and possibly the
	// custom query if they are all present into a single AND
	res.Query = joinAND(res.Query, negated, filters.Query)

	return res
}

func generateMatchFilters(filters config.MatchFilters) Criteria {
	res := Criteria{}
	if len(filters.From) > 0 {
		res.From = joinOR(filters.From...)
	}
	if len(filters.To) > 0 {
		res.To = joinOR(filters.To...)
	}
	if len(filters.Subject) > 0 {
		res.Subject = joinOR(filters.Subject...)
	}
	if len(filters.Has) > 0 {
		res.Query = joinOR(filters.Has...)
	}
	if len(filters.List) > 0 {
		c := fmt.Sprintf("list:%s", joinOR(filters.List...))
		res.Query = joinAND(res.Query, c)
	}
	return res
}

func generateNegatedFilters(filters config.MatchFilters) string {
	clauses := []string{}
	if len(filters.From) > 0 {
		c := fmt.Sprintf("-{from:%s}", joinOR(filters.From...))
		clauses = append(clauses, c)
	}
	if len(filters.To) > 0 {
		c := fmt.Sprintf("-{to:%s}", joinOR(filters.To...))
		clauses = append(clauses, c)
	}
	if len(filters.Subject) > 0 {
		c := fmt.Sprintf("-{subject:%s}", joinOR(filters.Subject...))
		clauses = append(clauses, c)
	}
	if len(filters.Has) > 0 {
		c := fmt.Sprintf("-%s", joinOR(filters.Has...))
		clauses = append(clauses, c)
	}
	if len(filters.List) > 0 {
		c := fmt.Sprintf("-{list:%s}", joinOR(filters.List...))
		clauses = append(clauses, c)
	}

	if len(clauses) == 0 {
		return ""
	}
	return strings.Join(clauses, " ")
}

func joinAND(a ...string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return a[0]
	}
	nonEmpty := []string{}
	for _, s := range a {
		if len(s) > 0 {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return strings.Join(nonEmpty, " ")
}

func joinOR(a ...string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return quote(a[0])
	}
	return fmt.Sprintf("{%s}", strings.Join(quoteStrings(a...), " "))
}

func quoteStrings(a ...string) []string {
	res := make([]string, len(a))
	for i, s := range a {
		res[i] = quote(s)
	}
	return res
}

func quote(a string) string {
	if strings.ContainsRune(a, ' ') && !quoted(a) {
		return fmt.Sprintf(`"%s"`, a)
	}
	return a
}

func quoted(a string) bool {
	return len(a) > 0 && a[0] == '"' && a[len(a)-1] == '"'
}

func generateActions(actions config.Actions) []Action {
	res := []Action{
		{
			Archive:       actions.Archive,
			Delete:        actions.Delete,
			MarkImportant: actions.MarkImportant,
			MarkRead:      actions.MarkRead,
			Category:      actions.Category,
		},
	}

	// Since every action can contain a single lable only, we might need to produce multiple actions
	for i, label := range actions.Labels {
		if i == 0 {
			// The first label can stay in the first action
			res[0].AddLabel = label
		} else {
			// All the subsequent labels need a separate action
			res = append(res, Action{AddLabel: label})
		}
	}

	return res
}

func combineCriteriaWithActions(criteria Criteria, actions []Action) Filters {
	// We have to duplicate the criteria for all the given actions
	res := make(Filters, len(actions))
	for i, action := range actions {
		res[i] = Filter{
			Criteria: criteria,
			Action:   action,
		}
	}
	return res
}