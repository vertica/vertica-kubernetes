#!/usr/bin/env python

# Copyright 2021 Vertica

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
To save about 900MB of space, we strip the package libraries in
/opt/vertica/packages.

Unfortunately, each directory in /opt/vertica/packages has two files: 
 - package.conf
 - ddl/isinstalled.sql

package.conf looks like this:

[Info]
Description=Amazon Web Services Package
Autoinstall=True
Version=11.0.0
md5sum=b22b4486faa8df8d70fc399ac5a85521

and isinstalled.sql looks like this:

SELECT (COUNT(0) = 5) FROM user_libraries
INNER JOIN user_library_manifest ON user_libraries.lib_name = user_library_manifest.lib_name
WHERE user_library_manifest.lib_name = 'awslib'
AND user_libraries.schema_name = 'public'
AND (user_libraries.md5_sum = 'b22b4486faa8df8d70fc399ac5a85521' 
        OR public.length('b22b4486faa8df8d70fc399ac5a85521') = 7);

by stripping the libraries, we've changed their checksums, so these
files are no longer accurate.

So, we patch the relevant files.

This runs in a pretty stripped-down environment, so we try to keep ourselves
to core python.
"""
import os
import os.path
import re
import sys
import subprocess

progname = sys.argv[0]

def parse_conf(dir):
    """
    Extract the Autoinstall and md5sum fields from the 
    package.conf file
    
    Args:
     - dir: directory name
    Returns:
     (autoinstall value, checksum value), where
         autoinstall value is "True" or "False"
    """
    md5pat = re.compile('^md5sum=(.*)$')
    autopat = re.compile('^Autoinstall=(.*)$')
    checksum = None
    autoinstall = None
    
    with open(dir + '/package.conf', 'r') as fp:
        for line in fp:
            m = md5pat.match(line)
            if m:
                checksum = m.group(1)
            else:
                m = autopat.match(line)
                if m:
                    autoinstall = m.group(1)
            if autoinstall and checksum:
                return (autoinstall, checksum)
    return (autoinstall, checksum)

def patch_file(fname, old_checksum, new_checksum):
    """
    Replace the old checksum with the new checksum in file
    Args:
     - fname: name of file to patch
     - old_checksum: *string* containing the old checksum value to be replaced
     - new_checksum: *string* containing the new checksum to insert
    Returns:
     None

    Backs fname up as fname~
    """
    print(f'    file {fname} {old_checksum} -> {new_checksum}')
    file_new = fname + '.new'
    file_backup = fname + '~'
    xsumpat = re.compile(old_checksum)
    with open(fname, 'r') as ifp:
        with open(file_new, 'w') as ofp:
            for line in ifp:
                edited = re.sub(xsumpat, new_checksum, line)
                print(f'{edited}', file=ofp, end='')
    
    try:
        os.remove(file_backup)
    except FileNotFoundError:
        # the first time through, the file won't be found
        pass
    os.remove(fname)
    os.rename(file_new, fname)

def patch_dir(dir, old_checksum):
    """
    Patch the package.conf and ddl/isinstalled.sql files in directory dir
    Args:
     - dir: the package directory
     - old_checksum: the old checksum to be replaced with the new checksum 
    Returns:
     None
    """
    libname = 'lib' + os.path.basename(dir) + '.so'
    xsum_out = str(subprocess.check_output(['md5sum', dir + '/lib/' + libname]))
    new_checksum = xsum_out.split(' ')[0]
    # converting byte-like object to string prefixes the string with "b'"
    if new_checksum.startswith("b'"):
        new_checksum = new_checksum[2:]
    patch_file(dir + '/package.conf', old_checksum, new_checksum)
    patch_file(dir + '/ddl/isinstalled.sql', old_checksum, new_checksum)

def process_dir(dir):
    """
    Process a package directory:
     - figure out if the package is auto-installed
     - if so, patch its checksum 
    Skips packages that aren't automatically installed (maybe it 
    shouldn't --- how to install those, after all?)
    """
    (autoinstall, checksum) = parse_conf(dir)
    if checksum:
        print(f'patching directory {dir}')
        patch_dir(dir, checksum)
    else:
        # no package.conf file, or no checksum in it --> probably not set up
        # with standard package mechanism.
        print(f'skipping directory {dir} with no checksum in package.conf file')

def main(argv):
    """
    Iterate over the list of files passed as arguments
    """
    if len(argv) < 2:
        print(f'Usage: {progname} packagedir1 [packagedir2...]', file=sys.stderr)
        sys.exit(1)
    # argv[0] is the command name
    for dir in argv[1:]:
        process_dir(dir)

if __name__ == '__main__':
    main(sys.argv)
    sys.exit(0)
