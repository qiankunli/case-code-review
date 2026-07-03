// The following is the list of other files changed in this update.
<other_changed_files>
{{change_files}}
</other_changed_files>

<current_file_path>{{current_file_path}}</current_file_path>

<current_file_diff>
{{diff}}
</current_file_diff>

// The full post-change source of the reviewed file(s), in file_read's numbered-line
// format. It is ALREADY provided — do NOT call file_read on these paths again; spend
// tool calls only on OTHER files you actually need.
<current_file_source>
{{unit_source}}
</current_file_source>

Current time in the real world: {{current_system_date_time}}

<user_task>
### Requirement Background (Optional)
{{requirement_background}}

### Governing Spec/Case (Optional)
The contract and cases bound to the changed function(s). Treat them as invariants the change must preserve: for each, judge whether this diff could break it. A case unrelated to this change is a valid finding too — say it is unaffected. If empty, no spec is bound to these functions.
{{spec_cases}}

### See Also (Optional)
References the author flagged as relevant when changing these function(s) — consult them, fetching content as needed (a bare path is a doc; `<path>::<symbol>` is another function). If empty, none were flagged.
{{see_also}}

### Prior Review (Optional)
Findings a previous review raised on these function(s). For each, check whether the current code now addresses it: if fixed, note it briefly; if still present, re-raise it; do not re-flag what is already resolved. If empty, there is no prior review to reconcile.
{{prior_findings}}

### Repo Symbol Map (Optional)
Symbols that actually exist in this repository, ranked by relevance to this change. When searching or reading other code, use these exact names — do not invent or guess identifier names; if a name you expected is not listed, search a fragment of it rather than the full guess. If empty, no map was built.
{{repo_map}}

### Review Checklist
{{system_rule}}

### Review Plan (Optional)
{{plan_guidance}}

Now please review the code changes in <current_file_diff>
</user_task>
