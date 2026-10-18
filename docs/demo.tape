# Run from the repository root with `make demo`, which builds tegami and
# prepares bin/demo. This records a real session; codenames and keys differ
# on every run.
Output docs/assets/terminal.gif

Require tegami
Require jq
Require bash

Set Shell bash
Set FontFamily "DejaVu Sans Mono"
Set FontSize 34
Set Width 2560
Set Height 1440
Set Padding 42
Set Margin 32
Set MarginFill "#0a0a0a"
Set BorderRadius 26
Set WindowBarSize 96
Set WindowBar Colorful
Set Theme "Catppuccin Mocha"
Set Framerate 10
Set TypingSpeed 35ms
Set CursorBlink false

# Hide local shell configuration and use a short, consistent prompt.
Hide
Type `cd bin/demo; unset PROMPT_COMMAND NO_COLOR; export TERM=xterm-256color; PS1='\033[38;5;111m❯\033[0m '; clear`
Enter
Sleep 500ms
Show

# Generate a password and keep it in a file, out of the shell history.
Type "tegami password > password.txt; cat password.txt"
Enter
Wait+Line@10s /^❯\s*$/
Sleep 1500ms

# Encrypt a file. The codename is the delivery's only visible name.
Type "tegami pack --input 'Q3 forecast.csv' --output delivery.html --format html < password.txt"
Enter
Wait+Line@20s /^❯\s*$/
Sleep 1500ms

# Without the password, a delivery shows only its codename and size.
Type "tegami inspect --input delivery.html"
Enter
Wait+Line@10s /^❯\s*$/
Sleep 1500ms

# Open it again, as a script would.
Type "tegami open --input delivery.html --output opened --json < password.txt | jq"
Enter
Wait+Screen@20s /"output"/
Sleep 1s
Screenshot docs/assets/terminal.png
Sleep 3s

# A wrong password exits 3, so scripts can tell it apart from other failures.
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type `echo 'not the password' | tegami open --input delivery.html --output -; echo "exit $?"`
Enter
Wait+Screen@20s /exit 3/
Sleep 2500ms

# Pipes work too: encrypt a folder straight from tar into a streaming blob.
Type "tar -c reports | tegami pack --input - --name reports.tar --output - --format blob --password-file password.txt > reports.tegami"
Enter
Wait+Line@20s /^❯\s*$/
Sleep 1s
Type "ls -lh reports.tegami | awk '{print $5, $9}'"
Enter
Wait+Line@10s /^❯\s*$/
Sleep 4s
