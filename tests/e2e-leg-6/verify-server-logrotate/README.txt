This is a test suite that verifies the server logrotate
Design: https://confluence.verticacorp.com/pages/viewpage.action?spaceKey=DEV&title=Spec+for+logrotate+in+the+Server
Step 10 is most important to verify server logrotate
1. Start by running it with the default values
LogRotateInterval: interval format string, default value is 8 hours
LogRotateMaxSize: size format string, integer + case-insensitive unit suffix(K, M, G, T), default is 100M
LogRotateMaxAge: interval format string, default value is 7 days
EnableLogRotate (true by default)
2. Customize LogRotateMaxSize and LogRotateMaxAge to see if they work as expected
    - Run with customized LogRotateMaxSize set to 1K. Only vertica.log is rotated, and UDXLogs is not rotated. Verify the 'need_rotation' column is true, max_size=1024 in dc table
    - Run with customized LogRotateMaxAge set to 1s. Again, only vertica.log is rotated, and UDXLogs is not rotated. Verify the 'need_rotation' column is true, max_age=00:00:01 in dc table
3. clean up the config param