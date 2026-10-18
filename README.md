# Cannon Contact Forms Extension

Adds configurable contact forms to Cannon. Forms can be rendered as pages or blocks, use JSON Forms schemas for field definitions, store submissions, and optionally send notifications through Shoutrrr.

## Features

- Admin interface for creating, editing, activating, and removing contact forms.
- Page support for routing directly to a selected form.
- Block support for rendering an active form in a template space.
- JSON Forms schema support for form fields.
- Optional captcha rendering through Cannon's captcha capability.
- Optional Shoutrrr notification URL for email or other delivery targets.
- Stored form entries for later review.
- Overridable built-in templates.

## Cannon Capabilities

This extension exposes:

- `/meta`: extension name, description, version, and update URL base.
- `/capabilities`: page, block, admin, configuration, help, templates, and data capabilities.
- `/admin`: contact form management UI.
- `/configuration`: global notification settings.
- `/page`: contact form page definitions and rendering.
- `/block`: contact form block definitions and rendering.
- `/data/contact/submit`: public form submission endpoint.
- `/templates`: built-in template metadata for overrides.
- `/help`: extension help content.
- `/install`: creates the extension tables.

## Configuration

Use Cannon's extension configuration screen to set a Shoutrrr service URL. The URL is optional; submissions are saved even when notifications are not configured.

Examples of Shoutrrr targets include SMTP, Slack, Discord, and other services supported by `containrrr/shoutrrr`.

## Usage

1. Install and activate `cannon-ext-contact` in Cannon.
2. Open the extension admin screen.
3. Create a contact form with a name, introduction, recipient, status, and JSON Forms schema.
4. Add the form to a route using the Contact Form page type, or add a Contact Form block to a template space.

## Templates

The extension ships with templates under `templates/contact`. Cannon can list these templates and create theme overrides so site-specific markup can replace the built-in output.

## Releases

The extension reports:

- Name: `cannon-ext-contact`
- Version: `0.2.1`
- Update URL base: `https://github.com/rob121/cannon-ext-contact/releases/download`

GitHub releases should include a `cannon-extension.json` manifest plus platform binaries.
