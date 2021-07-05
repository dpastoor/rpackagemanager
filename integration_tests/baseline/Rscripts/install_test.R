# get the installed packages only, not base and recommended packages
ip <- as.data.frame(installed.packages(priority = "NA"))[, c("Package", "Version")]
row.names(ip) <- NULL
print(ip)
