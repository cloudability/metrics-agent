#!/bin/bash
set -e

# Get the current year, previous year, and next year
current_year=$(date +'%Y')
prev_year=$((current_year - 1))
next_year=$((current_year + 1))

standard_comment="//"
py_comment="#"

old_line="© Copyright Apptio, an IBM Corp. $prev_year, $current_year"
new_line="© Copyright Apptio, an IBM Corp. $current_year, $next_year"

run_id=$(mktemp -d)

# Use direct string assignment for comments
comment=$standard_comment

addHeader() {
  local item=$1
  # Check file extension and set comment accordingly
  [[ "$item" =~ \.py$ ]] && comment=$py_comment || comment=$standard_comment

  # Update or add the header
  if grep -q "© Copyright Apptio" "$item"; then
    sed -i "" "s|$comment $old_line|$comment $new_line|g" "$item"
    echo "Found a header and updated the dates if needed for $item"
  else
    # No header was found, add it
    temp="$run_id/$item"
    mkdir -p "$(dirname "$temp")"
    echo "$comment $new_line" > "$temp"
    # Appending newline to ensure the comment isn't considered a Go doc comment
    echo >> "$temp"
    cat "$item" >> "$temp"
    mv "$temp" "$item"
    echo "Header added to $item"
  fi
}

# Function for recursive directory traversal using find
traverse_files() {
    # For each files that need are committing
    for item in "$@"
    do
      echo "Checking header for $item"

      if [[ ($item == *.java) || ($item == *.scala) || ($item == *.go) || ($item == *.py) || ($item == tf) || ($item == *.proto)]]; then
        addHeader "$item"
      else
        echo "Skipped adding header to $item as extension does not match"
      fi

    done
}

traverse_files "$@"

