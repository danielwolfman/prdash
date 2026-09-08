package tui

import (
	"fmt"
	"sort"
	"strings"
)

type rowGroup struct {
	Key      string
	Label    string
	Position int
	Size     int
}

// groupRows joins native stacks and branch dependencies among visible PRs.
// Groups retain the newest member's place in the feed; dependencies come first.
func groupRows(rows []Row) []Row {
	rows = append([]Row(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].PR.UpdatedAt.After(rows[j].PR.UpdatedAt) })
	edges := make([][]int, len(rows))
	neighbors := make([][]int, len(rows))
	for i := range rows {
		rows[i].group = rowGroup{}
		for j := range rows {
			a, b := rows[i].PR, rows[j].PR
			if i == j || a.RepoFullName != b.RepoFullName {
				continue
			}
			native := a.StackNumber > 0 && a.StackNumber == b.StackNumber
			branch := a.HeadRefName != "" && a.HeadRefName == b.BaseRefName && a.HeadRepoFullName == a.RepoFullName
			if (native && a.StackPosition < b.StackPosition) || (!native && branch) {
				edges[i] = append(edges[i], j)
				neighbors[i] = append(neighbors[i], j)
				neighbors[j] = append(neighbors[j], i)
			}
		}
	}
	seen := make([]bool, len(rows))
	var result []Row
	for start := range rows {
		if seen[start] {
			continue
		}
		members := []int{start}
		seen[start] = true
		for cursor := 0; cursor < len(members); cursor++ {
			for _, next := range neighbors[members[cursor]] {
				if !seen[next] {
					seen[next] = true
					members = append(members, next)
				}
			}
		}
		// A stable topological sort also keeps sibling branches in feed order.
		sort.Ints(members)
		indegree := make(map[int]int, len(members))
		for _, i := range members {
			for _, j := range edges[i] {
				indegree[j]++
			}
		}
		var ordered []int
		remaining := append([]int(nil), members...)
		for len(remaining) > 0 {
			chosen := -1
			for k, i := range remaining {
				if indegree[i] == 0 {
					chosen = k
					break
				}
			}
			if chosen < 0 {
				// Branch bases can form a cycle; keep those PRs visible without
				// pretending a dependency order exists.
				break
			}
			i := remaining[chosen]
			ordered = append(ordered, i)
			remaining = append(remaining[:chosen], remaining[chosen+1:]...)
			for _, j := range edges[i] {
				indegree[j]--
			}
		}
		cyclic := len(remaining) > 0
		ordered = append(ordered, remaining...)
		stackNumbers := map[int]bool{}
		minNumber := rows[start].PR.Number
		for _, i := range members {
			minNumber = min(minNumber, rows[i].PR.Number)
			if n := rows[i].PR.StackNumber; n > 0 {
				stackNumbers[n] = true
			}
		}
		label := "Branch stack"
		var numbers []int
		for n := range stackNumbers {
			numbers = append(numbers, n)
		}
		sort.Ints(numbers)
		if len(numbers) > 0 {
			var names []string
			for _, n := range numbers {
				names = append(names, fmt.Sprintf("#%d", n))
			}
			label = "GitHub stack " + strings.Join(names, " + ")
		}
		if cyclic {
			label += " · cyclic branch bases"
		}
		for position, i := range ordered {
			row := rows[i]
			if len(members) > 1 || len(numbers) > 0 {
				row.group = rowGroup{Key: fmt.Sprintf("%s#%d", row.PR.RepoFullName, minNumber), Label: label, Position: position + 1, Size: len(members)}
			}
			result = append(result, row)
		}
	}
	return result
}
