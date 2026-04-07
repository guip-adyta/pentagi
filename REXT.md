### **Extensions**

> Development

- **OSINT** tool + agent type

<pre>Execution Flow
 1. | User prompt: `OSINT xyz.com`
 2. | `osint` tool trigger (flow hijack — CreateFlow + GetPrimaryExecutor)
    | \
    |  - Sys prompt: osint.tmpl
    |  - Qs  prompt: question_osint.tmpl
    |
    |(OSINT Agent loop)
 3. | GetSubtaskOsintHandler (mirror of: GetPentesterHandler)
 4. | performOsint (mirrof of: performPentester)
</pre>

Mirror of `Pentester` Agent type but with a custom override system prompt (`osint.tmpl`/`question_osint.tmpl`). Key methods: `CreateFlow`/`GetPrimaryExecutor`/`GetSubtaskOsintHandler`/`performOsint`. 

User prompts must include `OSINT:` / `osint` to trigger osint tool and 
agent type.