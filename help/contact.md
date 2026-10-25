# Managing Contact Forms

Use the extension admin page to add, edit, activate, deactivate, or delete contact forms.

## Form Settings

Each contact form stores:

- Form name
- Introduction, edited with the visual editor and shown as HTML above the public form
- Mail To address
- Active or inactive status
- JSON Forms data schema
- JSON Forms UI schema

## JSON Forms

The data schema defines the submitted data shape, field types, and required fields.

The UI schema defines layout, controls, labels, and options.

The default form includes `name`, `email`, and `message` fields.

## Templates

The extension embeds templates under `templates/contact`.

Sites can override templates in Cannon's template directory using the `extension/` prefix. For example:

- `extension/contact/form.html`
- `extension/contact/admin_list.html`
- `extension/contact/admin_form.html`

## Rendering A Form On A Route

The extension exposes a `contact-form` page.

In Cannon, create or edit a route under **System -> Routes**:

- Type: Extension
- Extension: Contact Forms
- Page: Contact Form (`contact-form`)
- Contact Form: select an active contact form

The selected form is stored as route metadata and passed to the extension as `page_data.form_id`.

No submission route is required by default. The rendered form posts to Cannon's extension data URL:

```text
/ext/{route_hash}/contact/submit
```

Cannon proxies that URL to the extension's `contact/submit` data handler, which stores the submission in the contact entries table, optionally sends the configured Shoutrrr notification, and redirects back to the form route with `sent=1`.

If you want a friendly public path, you may create a second route for submissions:

- Path: `/contact/submit`
- Type: Extension Endpoint
- Extension: Contact Forms
- Endpoint: Submit Contact Form (`submit`)

Then override the form template to use that action path.

## Notifications

The extension configuration stores a single `shoutrrr_url`. When configured, saved entries are sent through Shoutrrr using that URL.

## Rendering A Form In A Block

The extension exposes a `contact-form` block.

In Cannon, create a block under **System -> Blocks**:

- Type: Extension
- Extension: Contact Forms
- Block: Contact Form (`contact-form`)
- Contact Form: select an active contact form
- Space: the template block space, such as `footer` or `sidebar`

Then render that space from a frontend template:

```gotemplate
{{block "footer"}}
```

If the selected form is inactive or missing, the block renders nothing.

Block-rendered forms also post to the route-free extension data URL by default.
