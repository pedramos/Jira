Jira is an acme based client for jira.

Write support is lightly tested, but updating issues works. Issue
transitions haven't been tested.  It's aiming for rough feature parity
with `rsc.io/github/issue`.

Some code cribbed from `rsc.io/github/issue`.

Jira can be started from the plumber. An example rule:

	type is text
	data matches '[A-Z]+-[0-9]+'
	data matches '((CORP)|(ABC))-[0-9]+'
	plumb to jira
	plumb client Jira https://<jiraserver>

The 'p' flag will disable attempting to talk to the plumber. Sending
a plumber message of type `exit` will cause Jira to close all of its
windows and exit.


Another plumber rule to help download attachements

	type is text
	src is Jira
	data matches 'https:/<jiraserver>/secure/attachment/.*'
	data matches 'https://<jiraserver>/secure/attachment/[0-9]+/(.*)'
	plumb start rc -c 'cd <tmpdir>/; curl -s -u ''<user:password>'' -O  '$0' && plumb <tmpdir>'$1
