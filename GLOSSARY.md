# equip

equip shows every extension a coding agent would load in a project and lets the user pick which ones are active there, one by one or through presets.

## Language

**Agent**:
A coding agent that equip manages: Claude Code or Codex.
_Avoid_: Tool, client, harness

**Extension**:
A skill, plugin, or MCP server that an agent can load into a session.
_Avoid_: Gear, item, add-on, capability

**Skill**:
An extension defined by a `SKILL.md` that an agent loads by description or by explicit call.

**Plugin**:
An extension that bundles other extensions and comes from a marketplace.

**MCP server**:
An extension that gives an agent tools over the Model Context Protocol.

**Content**:
A skill or MCP server that a plugin contains. A skill among a plugin's contents has no key of its own and follows its plugin.
_Avoid_: Component, part

**Marketplace**:
The source a plugin comes from. It is not an extension and has no state of its own.

**Location**:
A file or directory an agent reads an extension from.
_Avoid_: Source, origin, skill dir

**Project**:
The git repository equip runs in, taken at its root and shared by all its worktrees. Outside git, the directory equip runs in.

**State**:
How an extension takes part in a project's sessions: on, manual-only, or off.
_Avoid_: Status, enabled flag

**Manual-only**:
The state of a skill that the user can call by name but that puts nothing into the session's context.
_Avoid_: User-invocable-only, explicit-only

**Preset**:
A named set of extensions for one kind of project, such as Ruby or Accounting.
_Avoid_: Profile, bundle, loadout

**Override**:
A state the user set by hand for one extension in one project, which wins over the project's presets.
_Avoid_: Exception, pin

**Facet**:
A way to narrow the list of extensions by kind, agent, state, or change, shown with the count of extensions it keeps.
_Avoid_: Filter, category

## Relationships

- A **Plugin** contains zero or more **Skills** and **MCP servers**, its **Contents**
- An **Extension** has one or more **Locations**, each read by one **Agent**
- A **Project** uses zero or more **Presets** and zero or more **Overrides**
- With one or more **Presets**, a **Project**'s active extensions are the union of their members plus its **Overrides**; every other extension is off
- An **Extension** has one **State** per **Project**, shared by every **Agent** that has it
- An **Extension** is identified by its kind and its name (for a **Plugin**, its name and **Marketplace**). Two **Agents** have the same extension when both match; skills that share a name are one extension, wherever they live
- A **Preset** or **Override** may name an **Extension** that is not installed on this machine; it takes effect once the extension is installed
- A **State** changed outside equip becomes an **Override** once the user saves
- A **Preset** can be shared across machines; a **Project**'s choice of **Presets** and its **Overrides** belong to one machine
- A **Preset** keeps its identity when renamed, so every **Project** that uses it still does
- A **Preset** changed or deleted outside equip applies to a **Project** once the user saves there; it never becomes an **Override**
- A **Project** keeps its **Presets** and **Overrides** when its repository moves
