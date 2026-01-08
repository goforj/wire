// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
	"golang.org/x/tools/go/types/typeutil"
)

type showCmd struct {
	tags    string
	profile profileFlags
}

// Name returns the subcommand name.
func (*showCmd) Name() string { return "show" }

// Synopsis returns a short summary of the subcommand.
func (*showCmd) Synopsis() string {
	return "describe all top-level provider sets"
}

// Usage returns the help text for the subcommand.
func (*showCmd) Usage() string {
	return `show [packages]

  Given one or more packages, show finds all the provider sets declared as
  top-level variables and prints what other provider sets they import and what
  outputs they can produce, given possible inputs. It also lists any injector
  functions defined in the package.

  If no packages are listed, it defaults to ".".
`
}

// SetFlags registers flags for the subcommand.
func (cmd *showCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	cmd.profile.addFlags(f)
}

// Execute runs the subcommand.
func (cmd *showCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	stop, err := cmd.profile.start()
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}
	defer stop()
	totalStart := time.Now()
	ctx = withTiming(ctx, cmd.profile.timings)

	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	loadStart := time.Now()
	info, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, packages(f))
	logTiming(cmd.profile.timings, "wire.Load", loadStart)
	if info != nil {
		keys := make([]wire.ProviderSetID, 0, len(info.Sets))
		for k := range info.Sets {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].ImportPath == keys[j].ImportPath {
				return keys[i].VarName < keys[j].VarName
			}
			return keys[i].ImportPath < keys[j].ImportPath
		})
		for i, k := range keys {
			if i > 0 {
				fmt.Println()
			}
			outGroups, imports := gather(info, k)
			fmt.Println(k)
			for _, imp := range sortSet(imports) {
				fmt.Printf("\t%s\n", imp)
			}
			for i := range outGroups {
				fmt.Printf("\tOutputs given %s:\n", outGroups[i].name)
				out := make(map[string]token.Pos, outGroups[i].outputs.Len())
				outGroups[i].outputs.Iterate(func(t types.Type, v interface{}) {
					switch v := v.(type) {
					case *wire.Provider:
						out[types.TypeString(t, nil)] = v.Pos
					case *wire.Value:
						out[types.TypeString(t, nil)] = v.Pos
					case *wire.Field:
						out[types.TypeString(t, nil)] = v.Pos
					default:
						panic("unreachable")
					}
				})
				for _, t := range sortSet(out) {
					fmt.Printf("\t\t%s\n", t)
					fmt.Printf("\t\t\tat %v\n", info.Fset.Position(out[t]))
				}
			}
		}
		if len(info.Injectors) > 0 {
			injectors := append([]*wire.Injector(nil), info.Injectors...)
			sort.Slice(injectors, func(i, j int) bool {
				if injectors[i].ImportPath == injectors[j].ImportPath {
					return injectors[i].FuncName < injectors[j].FuncName
				}
				return injectors[i].ImportPath < injectors[j].ImportPath
			})
			fmt.Println("\nInjectors:")
			for _, in := range injectors {
				fmt.Printf("\t%v\n", in)
			}
		}
	}
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("error loading packages")
		return subcommands.ExitFailure
	}
	logTiming(cmd.profile.timings, "total", totalStart)
	return subcommands.ExitSuccess
}

type outGroup struct {
	name    string
	inputs  *typeutil.Map // values are not important
	outputs *typeutil.Map // values are *wire.Provider, *wire.Value, or *wire.Field
}

// gather flattens a provider set into outputs grouped by the inputs
// required to create them. As it flattens the provider set, it records
// the visited named provider sets as imports.
// gather flattens a provider set into output groups and imports.
func gather(info *wire.Info, key wire.ProviderSetID) (_ []outGroup, imports map[string]struct{}) {
	set := info.Sets[key]
	hash := typeutil.MakeHasher()

	// Find imports.
	next := []*wire.ProviderSet{info.Sets[key]}
	visited := make(map[*wire.ProviderSet]struct{})
	imports = make(map[string]struct{})
	for len(next) > 0 {
		curr := next[len(next)-1]
		next = next[:len(next)-1]
		if _, found := visited[curr]; found {
			continue
		}
		visited[curr] = struct{}{}
		if curr.VarName != "" && !(curr.PkgPath == key.ImportPath && curr.VarName == key.VarName) {
			imports[formatProviderSetName(curr.PkgPath, curr.VarName)] = struct{}{}
		}
		next = append(next, curr.Imports...)
	}

	// Depth-first search to build groups.
	var groups []outGroup
	inputVisited := new(typeutil.Map) // values are int, indices into groups or -1 for input.
	inputVisited.SetHasher(hash)
	var stk []types.Type
	for _, k := range set.Outputs() {
		// Start a DFS by picking a random unvisited node.
		if inputVisited.At(k) == nil {
			stk = append(stk, k)
		}

		// Run DFS
	dfs:
		for len(stk) > 0 {
			curr := stk[len(stk)-1]
			stk = stk[:len(stk)-1]
			if inputVisited.At(curr) != nil {
				continue
			}
			switch pv := set.For(curr); {
			case pv.IsNil():
				// This is an input.
				inputVisited.Set(curr, -1)
			case pv.IsArg():
				// This is an injector argument.
				inputVisited.Set(curr, -1)
			case pv.IsProvider():
				// Try to see if any args haven't been visited.
				p := pv.Provider()
				allPresent := true
				for _, arg := range p.Args {
					if inputVisited.At(arg.Type) == nil {
						allPresent = false
					}
				}
				if !allPresent {
					stk = append(stk, curr)
					for _, arg := range p.Args {
						if inputVisited.At(arg.Type) == nil {
							stk = append(stk, arg.Type)
						}
					}
					continue dfs
				}

				// Build up set of input types, match to a group.
				in := new(typeutil.Map)
				in.SetHasher(hash)
				for _, arg := range p.Args {
					i := inputVisited.At(arg.Type).(int)
					if i == -1 {
						in.Set(arg.Type, true)
					} else {
						mergeTypeSets(in, groups[i].inputs)
					}
				}
				for i := range groups {
					if sameTypeKeys(groups[i].inputs, in) {
						groups[i].outputs.Set(curr, p)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, p)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case pv.IsValue():
				v := pv.Value()
				for i := range groups {
					if groups[i].inputs.Len() == 0 {
						groups[i].outputs.Set(curr, v)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				in := new(typeutil.Map)
				in.SetHasher(hash)
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, v)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case pv.IsField():
				// Try to see if the parent struct hasn't been visited.
				f := pv.Field()
				if inputVisited.At(f.Parent) == nil {
					stk = append(stk, curr, f.Parent)
					continue
				}
				// Build the input map for the parent struct.
				in := new(typeutil.Map)
				in.SetHasher(hash)
				i := inputVisited.At(f.Parent).(int)
				if i == -1 {
					in.Set(f.Parent, true)
				} else {
					mergeTypeSets(in, groups[i].inputs)
				}
				// Group all fields together under the same parent struct.
				for i := range groups {
					if sameTypeKeys(groups[i].inputs, in) {
						groups[i].outputs.Set(curr, f)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, f)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			default:
				panic("unreachable")
			}
		}
	}

	// Name and sort groups.
	for i := range groups {
		if groups[i].inputs.Len() == 0 {
			groups[i].name = "no inputs"
			continue
		}
		instr := make([]string, 0, groups[i].inputs.Len())
		groups[i].inputs.Iterate(func(k types.Type, _ interface{}) {
			instr = append(instr, types.TypeString(k, nil))
		})
		sort.Strings(instr)
		groups[i].name = strings.Join(instr, ", ")
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].inputs.Len() == groups[j].inputs.Len() {
			return groups[i].name < groups[j].name
		}
		return groups[i].inputs.Len() < groups[j].inputs.Len()
	})
	return groups, imports
}

// mergeTypeSets merges source keys into the destination set.
func mergeTypeSets(dst, src *typeutil.Map) {
	src.Iterate(func(k types.Type, _ interface{}) {
		dst.Set(k, true)
	})
}

// sameTypeKeys reports whether two maps contain the same keys.
func sameTypeKeys(a, b *typeutil.Map) bool {
	if a.Len() != b.Len() {
		return false
	}
	same := true
	a.Iterate(func(k types.Type, _ interface{}) {
		if b.At(k) == nil {
			same = false
		}
	})
	return same
}

// sortSet returns a sorted list of map keys as strings.
func sortSet(set interface{}) []string {
	rv := reflect.ValueOf(set)
	a := make([]string, 0, rv.Len())
	keys := rv.MapKeys()
	for _, k := range keys {
		a = append(a, k.String())
	}
	sort.Strings(a)
	return a
}

// formatProviderSetName renders the provider set name for display.
func formatProviderSetName(importPath, varName string) string {
	// Since varName is an identifier, it doesn't make sense to quote.
	return strconv.Quote(importPath) + "." + varName
}
