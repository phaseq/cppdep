package main

// Extract dependency information from a c++ project
//
// Projects are tagged by putting a file called "CMakeLists.txt" into their root

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type log_flags struct {
	components     []string
	warn_missing   bool
	warn_malformed bool
	show_incoming  bool
	show_outgoing  bool
}

func main() {
	root_dir := flag.String("root", ".", "root directory of project")
	warn_missing := flag.Bool("warn-missing", false, "warn about missing includes")
	warn_malformed := flag.Bool("warn-malformed", false, "warn about malformed includes")
	show_incoming := flag.Bool("show-incoming", false, "show files for incoming dependencies")
	show_outgoing := flag.Bool("show-outgoing", false, "show files for outgoing dependencies")
	flag.Parse()

	flags := log_flags{
		components:     flag.Args(),
		warn_missing:   *warn_missing,
		warn_malformed: *warn_malformed,
		show_incoming:  *show_incoming,
		show_outgoing:  *show_outgoing}

	project := read_files(*root_dir, flags)
	project.assign_files_to_components()
	project.generate_file_deps(flags)
	project.print_components(flags)
	//project.dbg_files()
}

type file struct {
	path          string
	include_paths []string

	component      *component
	incoming_links []*file
	outgoing_links []*file
}

func (f *file) print() {
	fmt.Printf("%s\n", f.path)
	fmt.Printf("  Component: %s\n", f.component.nice_name())

	/*fmt.Println("  Includes:")
	for _, include := range f.include_paths {
		fmt.Printf("    %s\n", include)
	}*/

	/*fmt.Println("  Incoming:")
	for _, fo := range f.incoming_links {
		fmt.Printf("    %s\n", fo.path)
	}*/

	fmt.Println("  Outgoing:")
	for _, fo := range f.outgoing_links {
		fmt.Printf("    %s\n", fo.path)
	}
}

type component struct {
	path  string
	files []*file
}

func (c *component) nice_name() string {
	if c.path == "" {
		return "."
	}
	return c.path
}

type dependency struct {
	component *component
	edges     []edge
}

type edge struct {
	from *file
	to   *file
}

func (c *component) linked_components() ([]dependency, []dependency) {
	incoming := make(map[*component][]edge)
	outgoing := make(map[*component][]edge)
	for f_index, f := range c.files {
		for _, in := range f.incoming_links {
			if in.component.path != c.path {
				incoming[in.component] = append(
					incoming[in.component], edge{from: in, to: c.files[f_index]})
			}
		}
		for _, out := range f.outgoing_links {
			if out.component.path != c.path {
				outgoing[out.component] = append(
					outgoing[out.component], edge{from: c.files[f_index], to: out})
			}
		}
	}
	in := make([]dependency, 0, len(incoming))
	for k := range incoming {
		in = append(in, dependency{component: k, edges: incoming[k]})
	}
	out := make([]dependency, 0, len(outgoing))
	for k := range outgoing {
		out = append(out, dependency{component: k, edges: outgoing[k]})
	}
	return in, out
}

func (c *component) print(flags log_flags) {
	fmt.Printf("%s (%d)\n", c.nice_name(), len(c.files))

	in, out := c.linked_components()
	sort.Slice(in, func(i, j int) bool {
		return in[i].component.path < in[j].component.path
	})
	sort.Slice(out, func(i, j int) bool {
		return out[i].component.path < out[j].component.path
	})

	fmt.Println("  Incoming:")
	for _, dep := range in {
		fmt.Printf("    %s\n", dep.component.nice_name())
		if flags.show_incoming {
			for _, e := range dep.edges {
				fmt.Printf("      %s -> %s\n", e.from.path, e.to.path)
			}
		}
	}

	fmt.Println("  Outgoing:")
	for _, dep := range out {
		fmt.Printf("    %s\n", dep.component.nice_name())
		if flags.show_outgoing {
			for _, e := range dep.edges {
				fmt.Printf("      %s -> %s\n", e.from.path, e.to.path)
			}
		}
	}

	/*fmt.Println("  Files:")
	for _, f := range c.files {
		fmt.Printf("   %s\n", f.path)
	}*/
}

type project struct {
	root       string
	files      []file
	components []component
}

func (p *project) rel_path(path string) string {
	rel_path := strings.TrimPrefix(strings.TrimPrefix(path, p.root), "/")
	return rel_path
}

func (p *project) print_components(flags log_flags) {
	for _, c := range p.components {
		should_print := len(flags.components) == 0
		for _, name := range flags.components {
			if name == c.nice_name() {
				should_print = true
				break
			}
		}
		if should_print {
			c.print(flags)
		}
	}
}

func (p *project) print_files() {
	for _, f := range p.files {
		f.print()
	}
}

func (p *project) assign_files_to_components() {
	for i_file, file := range p.files {
		// Iterate over prefixes of file path, to find the most specific component.
		// Assign to the most specific component.
		// Example: file path: "a/b/header.hpp"
		// candidate 1: a/b
		// candidate 2: a
		// candidate 3: ''
		idx := 0
		path := file.path
		for idx != -1 {
			idx = strings.LastIndex(path, "/")
			if idx != -1 {
				path = path[:idx]
			} else {
				path = ""
			}
			for i_c, c := range p.components {
				if c.path == path {
					p.components[i_c].files = append(c.files, &p.files[i_file])
					p.files[i_file].component = &p.components[i_c]
					idx = -1
					break
				}
			}
		}
	}
}

func (p *project) generate_file_deps(flags log_flags) {
	// map from possible include paths to corresponding files
	// for example: "a/b/header.h" could be included as "header.h", "b/header.h", and "a/b/header.h"
	// assumption here: normalized paths with unix slashes
	path_to_files := make(map[string][]*file)
	for i_file, file := range p.files {
		path := file.path
		path_to_files[path] = append(path_to_files[path], &p.files[i_file])
		for idx := strings.Index(path, "/"); idx != -1; idx = strings.Index(path, "/") {
			path = path[idx+1:]
			path_to_files[path] = append(path_to_files[path], &p.files[i_file])
		}
	}

	for i_file, file := range p.files {
		for _, include := range file.include_paths {
			deps, present := path_to_files[include]
			if present {
				// If a file can be included from the current solution, assume that it is.
				// This avoids adding dependencies to headers with name clashes (like StdAfx.h).
				is_present_in_this_component := false
				for _, dep := range deps {
					if dep.component == file.component {
						is_present_in_this_component = true
						break
					}
				}
				if !is_present_in_this_component {
					for _, dep := range deps {
						p.files[i_file].outgoing_links =
							append(p.files[i_file].outgoing_links, dep)

						dep.incoming_links =
							append(dep.incoming_links, &p.files[i_file])
					}
				}
			} else if flags.warn_missing {
				fmt.Printf("Include not found in %s: %s\n", file.path, include)
			}
		}
	}
}

func read_files(root_path string, flags log_flags) project {
	source_suffixes := []string{".cpp", ".hpp", ".c", ".h"}
	ignore_patterns := []string{".svn", "dev/tools"}

	root_path = strings.TrimSuffix(root_path, "/")

	project := project{root: root_path}

	err := filepath.Walk(project.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		for _, pattern := range ignore_patterns {
			if strings.Contains(path, pattern) {
				fmt.Printf("skipping: %s\n", path)
				return filepath.SkipDir
			}
		}
		if info.Name() == "CMakeLists.txt" {
			component_path := project.rel_path(strings.TrimSuffix(path, "/CMakeLists.txt"))
			project.components = append(project.components, component{path: component_path})
		}
		for _, suffix := range source_suffixes {
			if strings.HasSuffix(path, suffix) {
				include_paths := extract_includes(path, flags)
				new_file := file{path: project.rel_path(path), include_paths: include_paths}
				project.files = append(project.files, new_file)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", project.root, err)
		panic(err)
	}
	return project
}

func extract_includes(path string, flags log_flags) []string {
	fh, err := os.Open(path)
	check(err)
	defer fh.Close()

	var results []string

	r := bufio.NewScanner(bufio.NewReader(fh))
	for r.Scan() {
		if strings.HasPrefix(r.Text(), "#include") {
			line := r.Text()
			iStart := strings.IndexAny(line, "\"<")
			iEnd := strings.LastIndexAny(line, "\">")
			if iStart == -1 || iEnd == -1 || iStart >= iEnd {
				if flags.warn_malformed {
					fmt.Printf("malformed #include in %s: %s\n", path, line)
				}
				continue
			}
			include_path := line[(iStart + 1):iEnd]
			if strings.Contains(include_path, "\\") || strings.Contains(include_path, "..") {
				if flags.warn_malformed {
					fmt.Printf("malformed #include in %s: %s\n", path, include_path)
				}
				continue
			}
			results = append(results, include_path)
		}
	}

	return results
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}
