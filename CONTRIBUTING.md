Thank you for contributing to *vertica-kubernetes*!

This document guides you through the contribution process. There are a number of ways you can help:

 - [Bug Reports](#bug-reports)
 - [Feature Requests](#feature-requests)
 - [Code Contributions](#code-contributions)
 
# Bug Reports

If you find a bug, [submit an issue](https://github.com/vertica/vertica-kubernetes/issues) with a complete and reproducible bug report. If the issue can't be reproduced, it will be closed. If you opened an issue and then later resolved it on your own, comment on the issue and then close the issue.

For issues that are **not suitable** to be reported publicly on the GitHub issue system (e.g. security related issues), report your issues to [Vertica open source team](mailto:vertica-opensrc@microfocus.com) directly or file a case with Vertica support, if you have a support account.

# Feature Requests

The Vertica team is always open to suggestions -- feel free to share your ideas about how to improve *vertica-kubernetes*. To provide suggestions, [open an issue](https://github.com/vertica/vertica-kubernetes/issues) with details describing what feature(s) you would like added or changed.

# Code Contributions

## 1. Fork

Fork the [project on Github](https://github.com/vertica/vertica-kubernetes) and check out your copy locally:

```shell
git clone git@github.com:vertica/vertica-kubernetes.git
cd vertica-kubernetes
```

Your GitHub repository is called "origin" in Git. You should also setup **vertica/vertica-kubernetes** as an "upstream" remote:

```shell
git remote add upstream git@github.com:vertica/vertica-kubernetes.git
git fetch upstream
```

### Configure Git for the first time

Make sure git knows your [name](https://help.github.com/articles/setting-your-username-in-git/ "Set commit username in Git") and [email address](https://help.github.com/articles/setting-your-commit-email-address-in-git/ "Set commit email address in Git"):

```shell
git config --global user.name "John Smith"
git config --global user.email "email@example.com"
```

## 2. Branch

Create a new branch for the work with a descriptive name:

```shell
git checkout -b my-fix-branch
```

## 3. Set up the development environment

Refer to the [developer](DEVELOPER.md) document to setup your environment.

## 4. Implement your fix or feature

At this point, you're ready to make your changes.

### License Headers

Every file in this project must use the following Apache 2.0 header. Make sure that you replace the `[yyyy]` box on the first line with the appropriate year or years. If a copyright statement from another party is already present in the code, you should add the statement on top of the existing copyright statement:

```
Copyright (c) [yyyy] Micro Focus or one of its affiliates.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

### Commits

After you make changes on your branch, stage and commit as often as necessary:

```shell
git add .
git commit -m 'Add new e2e test for nodePort'
```

When writing the commit message:
- Describe precisely what the commit does.
- Limit each line of the commit message to 72 characters.
- Include the issue number `#N`, if the commit is related to an issue.

### Tests

Each code change should have a corresponding test that covers it.  We have two levels of tests: unit tests and end-to-end (e2e) tests.  Is is desirable to add both types of tests.  The e2e tests can take a while to run, so often times adding to an existing test is sufficient.

## 6. Push and Rebase

Publish your work on GitHub:

```shell
git push origin my-fix-branch
```

When you go to your GitHub page, you will notice that commits made on your local branch are pushed to the remote repository.

When upstream (vertica/vertica-kubernetes) has changed, you should rebase your work. The **rebase** command creates a linear history by moving your local commits onto the tip of the upstream commits.

Rebase your branch locally and force-push to your GitHub repository:

```shell
git checkout my-fix-branch
git fetch upstream
git rebase upstream/master
git push -f origin my-fix-branch
```


## 7. Create a Pull Request

When your work is ready to be pulled into *vertica-kubernetes*, you should create a pull request (PR) at GitHub.

A good pull request has:
 - commits with one logical change in each
 - well-formed messages for each commit
 - documentation and tests, if applicable

Go to your fork in GitHub, and [create a Pull Request](https://help.github.com/articles/creating-a-pull-request/) to `vertica:main`. 

### About CI
Unit tests are run automatically for each commit in the PR. End-to-end tests (e2e) are triggered when a new PR is opened. After you create the PR, you can run e2e tests manually for each subsequent commit by selecting the workflow among the list in the **Actions** section of Github.

### Sign the CLA
Before we accept a pull request, we ask contributors to sign a Contributor License Agreement (CLA) to confirm they have the right to donate the code. To electronically sign the CLA, follow the comment from **CLAassistant** on your pull request page. 

### Review
Pull requests are usually reviewed within a few days. To address comments:
1. Apply your changes in new commits.
2. Rebase your branch and force-push to the same branch.
3. Re-run the test suite to ensure that tests are still passing. 

To produce a clean commit history, our maintainers do squash merging after your PR is approved. Squash merging combines all of your PR commits into a single commit in the master branch.

After your pull request is merged, you can safely delete your branch and pull the changes from the upstream repository.