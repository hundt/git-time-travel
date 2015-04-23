git-time-travel
-------
Refer to a future commit sha1 in your commit message!

Inspired by the [Stripe CTF](https://stripe.com/blog/ctf3-launch)'s "gitcoin" challenge

###What is it?
Have you ever made a commit just to prepare for another commit, and wanted to make that clear in the first commit's message? Did you wish that you could refer to the future commit by sha1? Now you can!

### Demo
```
$ git commit --allow-empty -m 'I am the parent of ${CHILD_SHA1}'
[master 8207324] I am the parent of ${CHILD_SHA1}
$ git commit --allow-empty -m 'I am the child'
[master 1c4f972] I am the child
$ git log --oneline HEAD~2..HEAD
1c4f972 I am the child
8207324 I am the parent of ${CHILD_SHA1}
$ refer-to-child --parent=HEAD~1 --child=HEAD --prefix-length=6
$ git log --oneline HEAD~2..HEAD
9428c8c I am the child
cdd3ab5 I am the parent of 9428c8
```

###How do I use it?
Write `${CHILD_SHA1}` in your commit message whenever you want to refer to your child commit. Then after both the parent and the child are commited, run `refer-to-child` to update both commits.

###How does it work?
It simply iterates through sha1 prefixes, trying each one in the parent commit message and seeing if the (rebased) child ends up with a matching sha1. Once it finds a match, it creates the new commits and updates HEAD to point to them.

###Notes
* I don't know what will happen if the child is a merge commit.
* If you use the `--extra-header` option then it will add an extra header to the raw commit, thus giving infinite tries to guess a good prefix. I don't know what git clients/tools this plays well with.